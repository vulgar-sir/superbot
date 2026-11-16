package tunnel

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/google/uuid"
)

type SocksServer struct {
	hub   *Hub
	token string
}

func NewSocksServer(hub *Hub, token string) *SocksServer {
	return &SocksServer{hub: hub, token: token}
}

func (s *SocksServer) ListenAndServe(ctx context.Context, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		go s.handle(ctx, conn)
	}
}

func (s *SocksServer) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	if err := s.handshake(reader, conn); err != nil {
		return
	}
	deviceID, err := s.authenticate(reader, conn)
	if err != nil {
		return
	}
	device, err := s.hub.Get(deviceID)
	if err != nil {
		writeSocksReply(conn, 0x04)
		return
	}
	target, err := readSocksRequest(reader)
	if err != nil {
		writeSocksReply(conn, 0x01)
		return
	}
	openCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	session, err := device.Open(openCtx, target)
	if err != nil {
		writeSocksReply(conn, 0x05)
		return
	}
	writeSocksReply(conn, 0x00)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 32*1024)
		for {
			count, readErr := reader.Read(buffer)
			if count > 0 {
				if err := device.SendData(ctx, session.ID, buffer[:count]); err != nil {
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}()
	for {
		select {
		case data := <-session.Data:
			if _, err := conn.Write(data); err != nil {
				device.CloseConn(ctx, session.ID)
				return
			}
		case <-session.Closed():
			device.CloseConn(ctx, session.ID)
			return
		case <-done:
			device.CloseConn(ctx, session.ID)
			return
		}
	}
}

func (s *SocksServer) handshake(reader *bufio.Reader, conn net.Conn) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return err
	}
	if header[0] != 0x05 {
		return errors.New("unsupported socks version")
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return err
	}
	supportsAuth := false
	for _, method := range methods {
		if method == 0x02 {
			supportsAuth = true
		}
	}
	if !supportsAuth {
		_, _ = conn.Write([]byte{0x05, 0xff})
		return errors.New("username/password auth is required")
	}
	_, err := conn.Write([]byte{0x05, 0x02})
	return err
}

func (s *SocksServer) authenticate(reader *bufio.Reader, conn net.Conn) (uuid.UUID, error) {
	version, err := reader.ReadByte()
	if err != nil || version != 0x01 {
		return uuid.Nil, errors.New("invalid auth version")
	}
	usernameLength, err := reader.ReadByte()
	if err != nil {
		return uuid.Nil, err
	}
	username := make([]byte, int(usernameLength))
	if _, err := io.ReadFull(reader, username); err != nil {
		return uuid.Nil, err
	}
	passwordLength, err := reader.ReadByte()
	if err != nil {
		return uuid.Nil, err
	}
	password := make([]byte, int(passwordLength))
	if _, err := io.ReadFull(reader, password); err != nil {
		return uuid.Nil, err
	}
	if string(password) != s.token {
		_, _ = conn.Write([]byte{0x01, 0x01})
		return uuid.Nil, errors.New("invalid tunnel token")
	}
	deviceID, err := uuid.Parse(string(username))
	if err != nil {
		_, _ = conn.Write([]byte{0x01, 0x01})
		return uuid.Nil, errors.New("invalid device id")
	}
	_, err = conn.Write([]byte{0x01, 0x00})
	return deviceID, err
}

func readSocksRequest(reader *bufio.Reader) (string, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return "", err
	}
	if header[0] != 0x05 || header[1] != 0x01 {
		return "", errors.New("only CONNECT is supported")
	}
	var host string
	switch header[3] {
	case 0x01:
		address := make([]byte, 4)
		if _, err := io.ReadFull(reader, address); err != nil {
			return "", err
		}
		host = net.IP(address).String()
	case 0x03:
		length, err := reader.ReadByte()
		if err != nil {
			return "", err
		}
		address := make([]byte, int(length))
		if _, err := io.ReadFull(reader, address); err != nil {
			return "", err
		}
		host = string(address)
	case 0x04:
		address := make([]byte, 16)
		if _, err := io.ReadFull(reader, address); err != nil {
			return "", err
		}
		host = net.IP(address).String()
	default:
		return "", errors.New("unsupported address type")
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(reader, portBytes); err != nil {
		return "", err
	}
	port := binary.BigEndian.Uint16(portBytes)
	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

func writeSocksReply(conn net.Conn, code byte) {
	_, _ = conn.Write([]byte{0x05, code, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
}
