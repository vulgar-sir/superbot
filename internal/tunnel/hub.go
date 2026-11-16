package tunnel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

var ErrDeviceOffline = errors.New("device tunnel is offline")

type frame struct {
	Type    string `json:"type"`
	ID      uint32 `json:"id"`
	Target  string `json:"target,omitempty"`
	Data    string `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
}

type ClientConn struct {
	ID     uint32
	Data   chan []byte
	Opened chan error
	closed chan struct{}
	once   sync.Once
}

func (c *ClientConn) Closed() <-chan struct{} { return c.closed }

func (c *ClientConn) Close() {
	c.once.Do(func() { close(c.closed) })
}

type Device struct {
	conn  *websocket.Conn
	mu    sync.Mutex
	next  uint32
	conns map[uint32]*ClientConn
	done  chan struct{}
}

type Hub struct {
	mu      sync.Mutex
	devices map[uuid.UUID]*Device
}

func NewHub() *Hub {
	return &Hub{devices: make(map[uuid.UUID]*Device)}
}

func (h *Hub) Register(deviceID uuid.UUID, conn *websocket.Conn) *Device {
	device := &Device{conn: conn, conns: make(map[uint32]*ClientConn), done: make(chan struct{})}
	h.mu.Lock()
	if existing := h.devices[deviceID]; existing != nil {
		existing.failAll(ErrDeviceOffline)
		_ = existing.conn.Close(websocket.StatusGoingAway, "replaced")
	}
	h.devices[deviceID] = device
	h.mu.Unlock()
	return device
}

func (h *Hub) Unregister(deviceID uuid.UUID, device *Device) {
	h.mu.Lock()
	if h.devices[deviceID] == device {
		delete(h.devices, deviceID)
	}
	h.mu.Unlock()
	device.failAll(ErrDeviceOffline)
	close(device.done)
}

func (h *Hub) Get(deviceID uuid.UUID) (*Device, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	device := h.devices[deviceID]
	if device == nil {
		return nil, ErrDeviceOffline
	}
	return device, nil
}

func (d *Device) failAll(err error) {
	d.mu.Lock()
	conns := make([]*ClientConn, 0, len(d.conns))
	for _, conn := range d.conns {
		conns = append(conns, conn)
	}
	d.conns = make(map[uint32]*ClientConn)
	d.mu.Unlock()
	for _, conn := range conns {
		select {
		case conn.Opened <- err:
		default:
		}
		conn.Close()
	}
}

func (d *Device) write(ctx context.Context, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.conn.Write(ctx, websocket.MessageText, payload)
}

func (d *Device) Open(ctx context.Context, target string) (*ClientConn, error) {
	d.mu.Lock()
	d.next++
	id := d.next
	conn := &ClientConn{
		ID: id, Data: make(chan []byte, 64), Opened: make(chan error, 1), closed: make(chan struct{}),
	}
	d.conns[id] = conn
	d.mu.Unlock()
	if err := d.write(ctx, frame{Type: "open", ID: id, Target: target}); err != nil {
		d.remove(id)
		return nil, err
	}
	select {
	case err := <-conn.Opened:
		if err != nil {
			d.remove(id)
			return nil, err
		}
		return conn, nil
	case <-ctx.Done():
		d.remove(id)
		return nil, ctx.Err()
	case <-d.done:
		return nil, ErrDeviceOffline
	}
}

func (d *Device) SendData(ctx context.Context, id uint32, data []byte) error {
	return d.write(ctx, frame{Type: "data", ID: id, Data: base64.StdEncoding.EncodeToString(data)})
}

func (d *Device) CloseConn(ctx context.Context, id uint32) {
	_ = d.write(ctx, frame{Type: "close", ID: id})
	d.remove(id)
}

func (d *Device) remove(id uint32) {
	d.mu.Lock()
	conn := d.conns[id]
	delete(d.conns, id)
	d.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

func (d *Device) Handle(ctx context.Context, payload []byte) {
	var message frame
	if json.Unmarshal(payload, &message) != nil {
		return
	}
	d.mu.Lock()
	conn := d.conns[message.ID]
	d.mu.Unlock()
	if conn == nil {
		return
	}
	switch message.Type {
	case "open_ok":
		select {
		case conn.Opened <- nil:
		default:
		}
	case "open_error":
		err := errors.New(message.Message)
		if message.Message == "" {
			err = errors.New("device could not open target")
		}
		select {
		case conn.Opened <- err:
		default:
		}
		d.remove(message.ID)
	case "data":
		data, err := base64.StdEncoding.DecodeString(message.Data)
		if err != nil {
			return
		}
		select {
		case conn.Data <- data:
		case <-conn.closed:
		}
	case "close":
		d.remove(message.ID)
	}
}
