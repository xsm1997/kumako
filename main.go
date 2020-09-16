package main

import (
	"flag"
	"io"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server *ServerConfig `toml:"server"`
	Client *ClientConfig `toml:"client"`
	WindowSize int `toml:"window-size"`
	SubConnCount int `toml:"sub-conn-count"`
	DebugLevel int `toml:"debug-level"`
}

type ServerConfig struct {
	ListenAddress string `toml:"listen-address"`
	RedirectAddress string `toml:"redirect-address"`
}

type ClientConfig struct {
	ListenAddress string `toml:"listen-address"`
	ServerAddress string `toml:"server-address"`
}

var (
	confPath = flag.String("c", "./kumako.toml", "configuration file path")
	config Config
	endSig chan int

	running bool
	clientConn *net.TCPListener
	serverConn *net.TCPListener

	kumakoConnsLock sync.Mutex
	serverKumakoConns map[int]*KumakoConn
	clientKumakoConns map[int]*KumakoConn

	currentSessionID int
)

func clientPipeRoutine(conn *KumakoConn, tcpConn *net.TCPConn, sessionID int) {
	defer conn.Close()

	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		io.Copy(conn, tcpConn)
		conn.Close()
		tcpConn.Close()
		wg.Done()
	}()

	go func() {
		io.Copy(tcpConn, conn)
		conn.Close()
		tcpConn.Close()
		wg.Done()
	}()

	wg.Wait()

	kumakoConnsLock.Lock()
	clientKumakoConns[sessionID] = nil
	kumakoConnsLock.Unlock()
}

func clientAcceptRoutine(tcpConn *net.TCPConn) {
	currentSessionID++

	serverAddr, err := net.ResolveTCPAddr("tcp", config.Client.ServerAddress)
	if err != nil {
		LogE("ACCEPT", "Resolve TCP address error: %s!", err.Error())
		return
	}

	conn := NewKumakoConn()

	go clientPipeRoutine(conn, tcpConn, currentSessionID)

	for i:=0; i<config.SubConnCount; i++ {
		serverConn, err := net.DialTCP("tcp", nil, serverAddr)
		if err != nil {
			LogE("ACCEPT", "Dial server TCP error: %s!", err.Error())
			conn.Close()
			return
		}

		//handshake
		pkt := NewKumakoHandshakePacket(uint32(currentSessionID))
		buf := make([]byte, pkt.HeaderSize())
		pkt.Encode(buf)

		_, err = serverConn.Write(buf)
		if err != nil {
			LogE("ACCEPT", "Handshake send error: %s!", err.Error())
			conn.Close()
		}

		conn.AddSubConn(serverConn)
	}

	kumakoConnsLock.Lock()
	clientKumakoConns[currentSessionID] = conn
	kumakoConnsLock.Unlock()
}

func clientRoutine() {
	listenAddr, err := net.ResolveTCPAddr("tcp", config.Client.ListenAddress)
	if err != nil {
		LogF("CLIENT", "Resolve TCP address error: %s!", err.Error())
		os.Exit(1)
	}

	clientConn, err = net.ListenTCP("tcp", listenAddr)
	if err != nil {
		LogF("SERVER", "TCP Listen error: %s!", err.Error())
		os.Exit(1)
	}

	for running {
		tcpConn, err := clientConn.AcceptTCP()
		if err != nil {
			break
		}

		clientAcceptRoutine(tcpConn)
	}
}

func serverPipeRoutine(conn *KumakoConn, sessionID int) {
	redirectAddr, err := net.ResolveTCPAddr("tcp", config.Server.RedirectAddress)
	if err != nil {
		LogE("PIPE", "Resolve TCP address failed: %s!", err.Error())
		return
	}

	redirectConn, err := net.DialTCP("tcp", nil, redirectAddr)
	if err != nil {
		LogE("PIPE", "Dial TCP failed: %s!", err.Error())
		return
	}

	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		io.Copy(conn, redirectConn)
		conn.Close()
		redirectConn.Close()
		wg.Done()
	}()

	go func() {
		io.Copy(redirectConn, conn)
		conn.Close()
		redirectConn.Close()
		wg.Done()
	}()

	wg.Wait()

	kumakoConnsLock.Lock()
	serverKumakoConns[sessionID] = nil
	kumakoConnsLock.Unlock()
}

func serverAcceptRoutine(tcpConn *net.TCPConn) {
	//handshake
	pkt := &KumakoHandshakePacket{}
	buf := make([]byte, pkt.HeaderSize())

	nr := 0
	for nr < len(buf) {
		l, err := tcpConn.Read(buf[nr:])
		if l > 0 {
			nr += l
		}

		if err != nil {
			LogE("ACCEPT", "TCP handshake read error: %s!", err.Error())
			break
		}
	}

	if nr == len(buf) {
		//handshake complete
		_, err := pkt.Decode(buf)

		if err != nil {
			LogE("ACCEPT", "Unknown handshake message: %s!", err.Error())
			tcpConn.Close()
			return
		}

		LogI("ACCEPT", "Incoming connection (sessionID=%d).", pkt.SessionID)

		sessionID := int(pkt.SessionID)

		kumakoConnsLock.Lock()
		conn, ok := serverKumakoConns[sessionID]

		if !ok || conn == nil {
			conn = NewKumakoConn()
			serverKumakoConns[sessionID] = conn

			go serverPipeRoutine(conn, sessionID)
		}
		kumakoConnsLock.Unlock()

		conn.AddSubConn(tcpConn)
	}
}

func serverRoutine() {
	listenAddr, err := net.ResolveTCPAddr("tcp", config.Server.ListenAddress)
	if err != nil {
		LogF("SERVER", "Resolve TCP address error: %s!", err.Error())
		os.Exit(1)
	}

	serverConn, err = net.ListenTCP("tcp", listenAddr)
	if err != nil {
		LogF("SERVER", "TCP Listen error: %s!", err.Error())
		os.Exit(1)
	}

	for running {
		tcpConn, err := serverConn.AcceptTCP()
		if err != nil {
			break
		}

		serverAcceptRoutine(tcpConn)
	}
}

func main() {
	LogI("kumako", "Starting kumako...")

	flag.Parse()

	_, err := toml.DecodeFile(*confPath, &config)
	if err != nil {
		LogF("kumako", "Load conf file error. %s", err.Error())
		os.Exit(1)
	}

	if config.Client == nil && config.Server == nil || config.Client != nil && config.Server != nil {
		LogF("kumako", "client and server in conf file must have and only have one.")
		os.Exit(1)
	}

	if config.WindowSize <= 0 {
		config.WindowSize = 16
	}

	if config.SubConnCount <= 0 {
		config.SubConnCount = 8
	}

	endSig = make(chan int)
	serverKumakoConns = make(map[int]*KumakoConn)
	clientKumakoConns = make(map[int]*KumakoConn)
	currentSessionID = int(rand.Int31())
	running = true

	if config.Client != nil {
		go clientRoutine()
	} else if config.Server != nil {
		go serverRoutine()
	}

	sig := make(chan os.Signal)
	signal.Ignore(syscall.SIGPIPE)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sig:
		case <-endSig:
	}

	running = false
	if config.Client != nil {
		clientConn.Close()

		for _, conn := range clientKumakoConns {
			if conn != nil {
				conn.Close()
			}
		}
	} else if config.Server != nil {
		serverConn.Close()

		for _, conn := range serverKumakoConns {
			if conn != nil {
				conn.Close()
			}
		}
	}

	LogI("kumako", "exiting kumako...")
}
