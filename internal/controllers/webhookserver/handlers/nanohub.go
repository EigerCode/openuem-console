package handlers

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"howett.net/plist"
)

// mdmAuthenticatePayload represents the Authenticate check-in plist from Apple MDM.
type mdmAuthenticatePayload struct {
	UDID         string `plist:"UDID"`
	DeviceName   string `plist:"DeviceName"`
	Model        string `plist:"Model"`
	ModelName    string `plist:"ModelName"`
	OSVersion    string `plist:"OSVersion"`
	SerialNumber string `plist:"SerialNumber"`
	ProductName  string `plist:"ProductName"`
	Topic        string `plist:"Topic"`
	MessageType  string `plist:"MessageType"`
}

// Webhook event types from NanoHub/NanoMDM
type nanoHubWebhookEvent struct {
	Topic            string                  `json:"topic"`
	EventID          string                  `json:"event_id"`
	CreatedAt        time.Time               `json:"created_at"`
	AcknowledgeEvent *nanoHubAcknowledgeEvent `json:"acknowledge_event,omitempty"`
	CheckinEvent     *nanoHubCheckinEvent     `json:"checkin_event,omitempty"`
}

type nanoHubCheckinEvent struct {
	UDID         string            `json:"udid,omitempty"`
	EnrollmentID string            `json:"enrollment_id,omitempty"`
	Params       map[string]string `json:"url_params"`
	RawPayload   []byte            `json:"raw_payload"`
}

type nanoHubAcknowledgeEvent struct {
	UDID         string            `json:"udid,omitempty"`
	EnrollmentID string            `json:"enrollment_id,omitempty"`
	Status       string            `json:"status"`
	CommandUUID  string            `json:"command_uuid,omitempty"`
	Params       map[string]string `json:"url_params,omitempty"`
	RawPayload   []byte            `json:"raw_payload"`
}

// HandleNanoHubWebhook receives webhook events from NanoHub.
// POST /nanohub
func (h *Handler) HandleNanoHubWebhook(c echo.Context) error {
	var event nanoHubWebhookEvent
	if err := c.Bind(&event); err != nil {
		log.Printf("[ERROR]: could not parse NanoHub webhook event, reason: %v", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid event"})
	}

	switch event.Topic {
	case "mdm.Authenticate":
		if event.CheckinEvent != nil {
			log.Printf("[INFO]: Apple Device with id: %s has checked in (Authenticate)", event.CheckinEvent.UDID)

			// Parse the Authenticate plist to extract device info
			var payload mdmAuthenticatePayload
			if len(event.CheckinEvent.RawPayload) > 0 {
				decoder := plist.NewDecoder(bytes.NewReader(event.CheckinEvent.RawPayload))
				if err := decoder.Decode(&payload); err != nil {
					log.Printf("[WARN]: could not parse Authenticate plist (reason: %v), falling back to UDID from event", err)
					payload.UDID = event.CheckinEvent.UDID
				}
			} else {
				payload.UDID = event.CheckinEvent.UDID
			}

			deviceName := payload.DeviceName
			if deviceName == "" {
				deviceName = payload.UDID
			}

			// Extract enrollment token from URL params (passed by NanoHub from MDM ServerURL)
			enrollmentToken := ""
			if event.CheckinEvent.Params != nil {
				enrollmentToken = event.CheckinEvent.Params["token"]
			}

			if err := h.Model.UpsertNanoHubAgent(
				payload.UDID,
				deviceName,
				payload.Model,
				payload.SerialNumber,
				payload.OSVersion,
				enrollmentToken,
			); err != nil {
				log.Printf("[ERROR]: could not upsert NanoHub agent for %s, reason: %v", payload.UDID, err)
			} else {
				log.Printf("[INFO]: NanoHub agent registered/updated: %s (%s)", payload.UDID, deviceName)
			}
		}

	case "mdm.TokenUpdate":
		if event.CheckinEvent == nil {
			break
		}
		deviceID := event.CheckinEvent.UDID
		if deviceID == "" {
			deviceID = event.CheckinEvent.EnrollmentID
		}
		if deviceID == "" {
			log.Println("[ERROR]: NanoHub TokenUpdate event has no device ID")
			break
		}

		settings, err := h.Model.GetNanoHubSettings()
		if err != nil {
			log.Printf("[ERROR]: could not get NanoHub settings, reason: %v", err)
			break
		}

		// Enqueue DeviceInformation command
		if err := h.enqueueNanoHubCommand(settings.ServerURL, settings.Username, settings.Password, deviceID, "DeviceInformation"); err != nil {
			log.Printf("[ERROR]: could not enqueue DeviceInformation command, reason: %v", err)
		}

		// Enqueue InstalledApplicationList command
		if err := h.enqueueNanoHubCommand(settings.ServerURL, settings.Username, settings.Password, deviceID, "InstalledApplicationList"); err != nil {
			log.Printf("[ERROR]: could not enqueue InstalledApplicationList command, reason: %v", err)
		}

		// Enqueue UserList command
		if err := h.enqueueNanoHubCommand(settings.ServerURL, settings.Username, settings.Password, deviceID, "UserList"); err != nil {
			log.Printf("[ERROR]: could not enqueue UserList command, reason: %v", err)
		}

	case "mdm.Connect":
		if event.AcknowledgeEvent == nil {
			break
		}
		cmdUUID := event.AcknowledgeEvent.CommandUUID
		if cmdUUID == "" {
			break
		}

		cmd, err := h.Model.GetNanoHubCommand(cmdUUID)
		if err != nil {
			log.Printf("[DEBUG]: NanoHub command %s not tracked (may be idle or external), error: %v", cmdUUID, err)
			break
		}

		// Publish raw plist to NATS based on command type
		if h.NATSConnection != nil {
			switch cmd.Type {
			case "DeviceInformation":
				if err := h.NATSConnection.Publish("nanohub.deviceinfo", event.AcknowledgeEvent.RawPayload); err != nil {
					log.Printf("[ERROR]: could not publish NanoHub device info to NATS, reason: %v", err)
				}
			case "InstalledApplicationList":
				if err := h.NATSConnection.Publish("nanohub.installedapplicationslist", event.AcknowledgeEvent.RawPayload); err != nil {
					log.Printf("[ERROR]: could not publish NanoHub installed apps to NATS, reason: %v", err)
				}
			case "UserList":
				if err := h.NATSConnection.Publish("nanohub.userslist", event.AcknowledgeEvent.RawPayload); err != nil {
					log.Printf("[ERROR]: could not publish NanoHub users list to NATS, reason: %v", err)
				}
			}
		} else {
			log.Println("[ERROR]: NATS connection not available for NanoHub webhook")
		}

		// Remove completed command
		if err := h.Model.RemoveNanoHubCommand(cmdUUID); err != nil {
			log.Printf("[ERROR]: could not remove NanoHub command %s, reason: %v", cmdUUID, err)
		}

	case "mdm.CheckOut":
		if event.CheckinEvent != nil {
			deviceID := event.CheckinEvent.UDID
			log.Printf("[INFO]: Apple Device with id: %s has been removed after receiving a MDM Check Out command", deviceID)
		}
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// enqueueNanoHubCommand sends a command plist to NanoHub and tracks it.
func (h *Handler) enqueueNanoHubCommand(serverURL, username, password, deviceID, commandType string) error {
	cmdUUID := uuid.New().String()

	commandPlist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Command</key>
	<dict>
		<key>RequestType</key>
		<string>%s</string>
	</dict>
	<key>CommandUUID</key>
	<string>%s</string>
</dict>
</plist>`, commandType, cmdUUID)

	// Save command tracking
	if err := h.Model.SaveNanoHubCommand(cmdUUID, commandType, deviceID); err != nil {
		return fmt.Errorf("could not save command tracking: %w", err)
	}

	// Send to NanoHub API (NanoMDM endpoints are mounted under /api/v1/nanomdm/)
	baseURL := strings.TrimSuffix(serverURL, "/")
	url := fmt.Sprintf("%s/api/v1/nanomdm/enqueue/%s", baseURL, deviceID)
	req, err := http.NewRequest("PUT", url, bytes.NewReader([]byte(commandPlist)))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("User-Agent", "openuem-console")
	req.Header.Set("Content-Type", "application/xml")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not send command to NanoHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("NanoHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("[INFO]: enqueued %s command (%s) for device %s", commandType, cmdUUID, deviceID)
	return nil
}
