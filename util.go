package main

import "io"

func ReadFull(buf []byte, reader io.Reader) (int, error) {
	offset := 0
	var err error

	for offset < len(buf) {
		l := 0

		l, err = reader.Read(buf[offset:])
		if l > 0 {
			offset += l
		}

		if err != nil {
			break
		}
	}

	return offset, err
}

func WriteFull(buf []byte, writer io.Writer) (int, error) {
	offset := 0
	var err error

	for offset < len(buf) {
		l := 0

		l, err = writer.Write(buf[offset:])
		if l > 0 {
			offset += l
		}

		if err != nil {
			break
		}
	}

	return offset, err
}
