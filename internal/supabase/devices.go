package supabase

import (
	"encoding/json"
	"context"
	"net/url"
	"time"
)

// ---------------- devices ----------------

type Device struct {
	ID                string  `json:"id,omitempty"`
	UserID            string  `json:"user_id,omitempty"`
	DeviceName        string  `json:"device_name,omitempty"`
	PhoneNumber       *string `json:"phone_number,omitempty"`
	Status            string  `json:"status,omitempty"`
	QRCode            *string `json:"qr_code,omitempty"`
	PairingCode       *string `json:"pairing_code,omitempty"`
	ConnectionMethod  string  `json:"connection_method,omitempty"`
	PhoneForPairing   *string `json:"phone_for_pairing,omitempty"`
	SessionData       json.RawMessage `json:"session_data,omitempty"`
	AssignedServerID  *string `json:"assigned_server_id,omitempty"`
	ErrorMessage      *string `json:"error_message,omitempty"`
	APIKey            string  `json:"api_key,omitempty"`
	WebhookURL        *string `json:"webhook_url,omitempty"`
	LastConnectedAt   *string `json:"last_connected_at,omitempty"`
	UpdatedAt         string  `json:"updated_at,omitempty"`
}

// FetchAssignedDevices returns devices assigned to the given server, with status in the given list.
func (c *Client) FetchAssignedDevices(ctx context.Context, serverID string, statuses []string) ([]Device, error) {
	q := url.Values{}
	q.Set("assigned_server_id", "eq."+serverID)
	q.Set("status", buildIn(statuses))
	q.Set("select", "*")
	var rows []Device
	if err := c.do(ctx, "GET", "/rest/v1/devices?"+q.Encode(), nil, &rows, ""); err != nil {
		return nil, err
	}
	return rows, nil
}

// FetchUnassignedDevices returns devices with NULL assigned_server_id and status in the given list.
func (c *Client) FetchUnassignedDevices(ctx context.Context, statuses []string) ([]Device, error) {
	q := url.Values{}
	q.Set("assigned_server_id", "is.null")
	q.Set("status", buildIn(statuses))
	q.Set("select", "*")
	var rows []Device
	if err := c.do(ctx, "GET", "/rest/v1/devices?"+q.Encode(), nil, &rows, ""); err != nil {
		return nil, err
	}
	return rows, nil
}

// AtomicAssignDevice claims an unassigned device for the given server.
// Returns true if this server now owns the device.
func (c *Client) AtomicAssignDevice(ctx context.Context, deviceID, serverID string) (bool, error) {
	q := url.Values{}
	q.Set("id", "eq."+deviceID)
	q.Set("assigned_server_id", "is.null")
	body := map[string]any{
		"assigned_server_id": serverID,
		"updated_at":         time.Now().UTC().Format(time.RFC3339Nano),
	}
	var rows []Device
	if err := c.do(ctx, "PATCH", "/rest/v1/devices?"+q.Encode(), body, &rows, "return=representation"); err != nil {
		return false, err
	}
	return len(rows) == 1, nil
}

// PatchDevice merges fields into a device row.
func (c *Client) PatchDevice(ctx context.Context, id string, patch map[string]any) error {
	if _, ok := patch["updated_at"]; !ok {
		patch["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	q := url.Values{}
	q.Set("id", "eq."+id)
	return c.do(ctx, "PATCH", "/rest/v1/devices?"+q.Encode(), patch, nil, "")
}

// SetQR sets a fresh QR code for a device, clears pairing_code, marks connecting.
func (c *Client) SetQR(ctx context.Context, id, qr string) error {
	return c.PatchDevice(ctx, id, map[string]any{
		"qr_code":       qr,
		"pairing_code":  nil,
		"status":        "connecting",
		"error_message": nil,
	})
}

// SetPairing sets a fresh pairing code, clears qr_code.
func (c *Client) SetPairing(ctx context.Context, id, code string) error {
	return c.PatchDevice(ctx, id, map[string]any{
		"pairing_code":  code,
		"qr_code":       nil,
		"status":        "connecting",
		"error_message": nil,
	})
}

// SetConnected marks device connected with optional phone number.
func (c *Client) SetConnected(ctx context.Context, id string, phoneNumber string) error {
	patch := map[string]any{
		"status":            "connected",
		"qr_code":           nil,
		"pairing_code":      nil,
		"error_message":     nil,
		"last_connected_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if phoneNumber != "" {
		patch["phone_number"] = phoneNumber
	}
	return c.PatchDevice(ctx, id, patch)
}

// SetDisconnected marks device disconnected with optional reason.
func (c *Client) SetDisconnected(ctx context.Context, id, reason string) error {
	patch := map[string]any{
		"status":  "disconnected",
		"qr_code": nil,
	}
	if reason != "" {
		patch["error_message"] = reason
	}
	return c.PatchDevice(ctx, id, patch)
}

func buildIn(values []string) string {
	if len(values) == 0 {
		return "in.()"
	}
	out := "in.("
	for i, v := range values {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out + ")"
}
