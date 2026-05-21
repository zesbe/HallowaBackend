package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/zesbe/Sebar/internal/supabase"
)

// Manager owns the whatsmeow store and the per-device client map.
type Manager struct {
	container *sqlstore.Container
	sb        *supabase.Client
	log       *slog.Logger

	mu      sync.RWMutex
	clients map[string]*deviceClient // keyed by Supabase devices.id
}

type deviceClient struct {
	deviceID string
	device   *store.Device
	client   *whatsmeow.Client
	cancel   context.CancelFunc
}

func New(container *sqlstore.Container, sb *supabase.Client, log *slog.Logger) *Manager {
	return &Manager{
		container: container,
		sb:        sb,
		log:       log,
		clients:   make(map[string]*deviceClient),
	}
}

// Has reports whether a client exists for the given device id.
func (m *Manager) Has(deviceID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.clients[deviceID]
	return ok
}

// ActiveCount returns the number of currently held device clients.
func (m *Manager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}

// Disconnect tears down a single device client.
func (m *Manager) Disconnect(deviceID string) {
	m.mu.Lock()
	dc, ok := m.clients[deviceID]
	delete(m.clients, deviceID)
	m.mu.Unlock()
	if !ok {
		return
	}
	if dc.cancel != nil {
		dc.cancel()
	}
	if dc.client != nil {
		dc.client.Disconnect()
	}
}

// Shutdown disconnects everything.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.clients))
	for id := range m.clients {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Disconnect(id)
	}
}

// Connect ensures a whatsmeow client is running for the given Supabase device.
// If the device has no stored session, it starts a QR loop or pairing-code flow.
func (m *Manager) Connect(ctx context.Context, dev supabase.Device) error {
	if m.Has(dev.ID) {
		return nil
	}

	storeDevice, err := m.loadOrNewStoreDevice(ctx, dev)
	if err != nil {
		return fmt.Errorf("load store device: %w", err)
	}

	clientLog := waLog.Stdout("Client/"+shortID(dev.ID), "INFO", true)
	cl := whatsmeow.NewClient(storeDevice, clientLog)

	dctx, cancel := context.WithCancel(context.Background())
	dc := &deviceClient{
		deviceID: dev.ID,
		device:   storeDevice,
		client:   cl,
		cancel:   cancel,
	}

	m.mu.Lock()
	m.clients[dev.ID] = dc
	m.mu.Unlock()

	cl.AddEventHandler(m.makeEventHandler(dctx, dc))

	if cl.Store.ID != nil {
		// existing session: connect directly
		if err := cl.Connect(); err != nil {
			m.Disconnect(dev.ID)
			return fmt.Errorf("connect existing session: %w", err)
		}
		m.log.Info("reconnecting saved session", "device_id", dev.ID, "jid", cl.Store.ID.String())
		return nil
	}

	// fresh login: start QR or pairing flow
	go m.runFreshLogin(dctx, dc, dev)
	return nil
}

// loadOrNewStoreDevice maps a Supabase device to a whatsmeow store.Device.
// If the device has a phone number stored AND we have a saved session, we re-use it;
// otherwise we create a new blank device.
func (m *Manager) loadOrNewStoreDevice(ctx context.Context, dev supabase.Device) (*store.Device, error) {
	devices, err := m.container.GetAllDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("list store devices: %w", err)
	}
	if dev.PhoneNumber != nil && *dev.PhoneNumber != "" {
		want := normalizePhone(*dev.PhoneNumber)
		for _, d := range devices {
			if d.ID == nil {
				continue
			}
			if normalizePhone(d.ID.User) == want {
				return d, nil
			}
		}
	}
	return m.container.NewDevice(), nil
}

func normalizePhone(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			out = append(out, c)
		}
	}
	return string(out)
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// runFreshLogin handles the QR/pairing-code dance for a new session.
func (m *Manager) runFreshLogin(ctx context.Context, dc *deviceClient, dev supabase.Device) {
	qrCh, err := dc.client.GetQRChannel(ctx)
	if err != nil {
		m.log.Error("get qr channel failed", "device_id", dev.ID, "err", err)
		_ = m.sb.SetDisconnected(ctx, dev.ID, "qr channel: "+err.Error())
		m.Disconnect(dev.ID)
		return
	}
	if err := dc.client.Connect(); err != nil {
		m.log.Error("connect failed", "device_id", dev.ID, "err", err)
		_ = m.sb.SetDisconnected(ctx, dev.ID, "connect: "+err.Error())
		m.Disconnect(dev.ID)
		return
	}

	// If pairing was requested, fire off the pairing code request when the socket is ready.
	if dev.ConnectionMethod == "pairing" && dev.PhoneForPairing != nil && *dev.PhoneForPairing != "" {
		go m.requestPairingCode(ctx, dc, dev, *dev.PhoneForPairing)
	}

	for evt := range qrCh {
		switch evt.Event {
		case "code":
			m.log.Info("qr code generated", "device_id", dev.ID)
			if err := m.sb.SetQR(ctx, dev.ID, evt.Code); err != nil {
				m.log.Error("save qr failed", "device_id", dev.ID, "err", err)
			}
		case "success":
			m.log.Info("qr scan success", "device_id", dev.ID)
			return
		case "timeout":
			m.log.Warn("qr timeout", "device_id", dev.ID)
			_ = m.sb.SetDisconnected(ctx, dev.ID, "qr timeout")
			m.Disconnect(dev.ID)
			return
		default:
			m.log.Info("qr event", "device_id", dev.ID, "event", evt.Event)
		}
	}
}

func (m *Manager) requestPairingCode(ctx context.Context, dc *deviceClient, dev supabase.Device, phone string) {
	// give the socket a moment to be ready
	timer := time.NewTimer(800 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	clean := normalizePhone(phone)
	if clean == "" {
		_ = m.sb.SetDisconnected(ctx, dev.ID, "invalid phone for pairing")
		return
	}
	code, err := dc.client.PairPhone(ctx, clean, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		m.log.Error("pair phone failed", "device_id", dev.ID, "err", err)
		_ = m.sb.SetDisconnected(ctx, dev.ID, "pairing: "+err.Error())
		return
	}
	formatted := strings.ToUpper(code)
	m.log.Info("pairing code generated", "device_id", dev.ID, "code", formatted)
	if err := m.sb.SetPairing(ctx, dev.ID, formatted); err != nil {
		m.log.Error("save pairing code failed", "device_id", dev.ID, "err", err)
	}
}

func (m *Manager) makeEventHandler(ctx context.Context, dc *deviceClient) func(any) {
	return func(rawEvt any) {
		switch e := rawEvt.(type) {
		case *events.Connected:
			phone := ""
			if dc.client.Store.ID != nil {
				phone = dc.client.Store.ID.User
			}
			m.log.Info("device connected", "device_id", dc.deviceID, "phone", phone)
			if err := m.sb.SetConnected(ctx, dc.deviceID, phone); err != nil {
				m.log.Error("set connected failed", "device_id", dc.deviceID, "err", err)
			}
		case *events.LoggedOut:
			m.log.Warn("device logged out", "device_id", dc.deviceID, "reason", e.Reason.String())
			_ = m.sb.SetDisconnected(ctx, dc.deviceID, "logged out: "+e.Reason.String())
			m.Disconnect(dc.deviceID)
		case *events.Disconnected:
			m.log.Warn("device disconnected", "device_id", dc.deviceID)
		case *events.PairSuccess:
			m.log.Info("pair success", "device_id", dc.deviceID, "id", e.ID.String())
		case *events.StreamReplaced:
			m.log.Warn("stream replaced (another device with same JID)", "device_id", dc.deviceID)
			_ = m.sb.SetDisconnected(ctx, dc.deviceID, "stream replaced")
			m.Disconnect(dc.deviceID)
		}
	}
}

// SendText sends a plain text message via the named device.
func (m *Manager) SendText(ctx context.Context, deviceID, toJID, text string) (string, error) {
	m.mu.RLock()
	dc, ok := m.clients[deviceID]
	m.mu.RUnlock()
	if !ok {
		return "", errors.New("device not connected")
	}
	jid, err := types.ParseJID(toJID)
	if err != nil {
		return "", fmt.Errorf("parse jid: %w", err)
	}
	resp, err := dc.client.SendMessage(ctx, jid, &waProto.Message{
		Conversation: proto.String(text),
	})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}
