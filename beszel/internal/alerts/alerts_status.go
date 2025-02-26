package alerts

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type alertTask struct {
	action      string // "schedule" or "cancel"
	systemName  string
	alertRecord *core.Record
	delay       time.Duration
}

type alertInfo struct {
	systemName  string
	alertRecord *core.Record
	expireTime  time.Time
}

// statusAlertWorker is a long-running goroutine that processes alert tasks
func (am *AlertManager) statusAlertWorker() {
	// 13 just to try to avoid other scheduled updates
	tick := time.Tick(13 * time.Second)
	for {
		select {
		case <-am.stopChan:
			return
		case task := <-am.alertQueue:
			switch task.action {
			case "schedule":
				// Schedule a new alert
				expireTime := time.Now().Add(task.delay)
				am.pendingAlerts.Store(task.alertRecord.Id, &alertInfo{
					systemName:  task.systemName,
					alertRecord: task.alertRecord,
					expireTime:  expireTime,
				})
			case "cancel":
				// Cancel an existing alert
				am.pendingAlerts.Delete(task.alertRecord.Id)
				// case "process":
				// 	// Process an alert immediately
				// 	am.sendStatusAlert("down", task.systemName, task.alertRecord)
				// 	am.pendingAlerts.Delete(task.alertRecord.Id)
			}
		case <-tick:
			// Check for expired alerts every tick
			now := time.Now()
			for key, value := range am.pendingAlerts.Range {
				info := value.(*alertInfo)
				if now.After(info.expireTime) {
					// Alert has expired, process it
					am.sendStatusAlert("down", info.systemName, info.alertRecord)
					am.pendingAlerts.Delete(key)
				}
			}
		}
	}
}

// Stop gracefully shuts down the AlertManager
func (am *AlertManager) Stop() {
	close(am.stopChan)
}

// HandleStatusAlerts manages the logic when a system status changes.
func (am *AlertManager) HandleStatusAlerts(newStatus string, oldSystemRecord *core.Record) error {
	var statusChanged bool
	switch newStatus {
	case "up":
		statusChanged = oldSystemRecord.GetString("status") == "down"
	case "down":
		statusChanged = oldSystemRecord.GetString("status") == "up"
	}
	if !statusChanged {
		return nil
	}

	alertRecords, err := am.getSystemStatusAlerts(oldSystemRecord.Id)
	if err != nil {
		return err
	}
	if len(alertRecords) == 0 {
		return nil
	}

	systemName := oldSystemRecord.GetString("name")
	switch newStatus {
	case "down":
		am.handleSystemDown(systemName, alertRecords)
	case "up":
		am.handleSystemUp(systemName, alertRecords)
	}
	return nil
}

// getSystemStatusAlerts retrieves all "Status" alert records for a given system ID.
func (am *AlertManager) getSystemStatusAlerts(systemID string) ([]*core.Record, error) {
	alertRecords, err := am.app.FindAllRecords("alerts", dbx.HashExp{
		"system": systemID,
		"name":   "Status",
	})
	if err != nil {
		return nil, err
	}
	return alertRecords, nil
}

// handleSystemDown manages the logic when a system status changes to "down".
// It schedules delayed alerts for each alert record.
func (am *AlertManager) handleSystemDown(systemName string, alertRecords []*core.Record) {
	// log.Println("system down")
	for _, alertRecord := range alertRecords {
		min := max(1, alertRecord.GetInt("min"))
		// Add 10 seconds to give a little buffer in case update succeeds
		delayToNotification := time.Duration(min)*time.Minute + time.Second*10

		// Check if alert is already scheduled
		if _, exists := am.pendingAlerts.Load(alertRecord.Id); exists {
			continue
		}

		// Schedule a new alert
		am.alertQueue <- alertTask{
			action:      "schedule",
			systemName:  systemName,
			alertRecord: alertRecord,
			delay:       delayToNotification,
		}
	}
}

// handleSystemUp manages the logic when a system status changes to "up".
// It cancels any pending alerts and sends "up" alerts.
func (am *AlertManager) handleSystemUp(systemName string, alertRecords []*core.Record) {
	// log.Println("system up")
	for _, alertRecord := range alertRecords {
		alertRecordID := alertRecord.Id
		// If alert exists for record, delete and continue (down alert not sent)
		if _, exists := am.pendingAlerts.Load(alertRecordID); exists {
			am.alertQueue <- alertTask{
				action:      "cancel",
				alertRecord: alertRecord,
			}
			continue
		}
		// No alert scheduled for this record, send "up" alert
		if err := am.sendStatusAlert("up", systemName, alertRecord); err != nil {
			am.app.Logger().Error("Failed to send alert", "err", err.Error())
		}
	}
}

// sendStatusAlert sends a status alert ("up" or "down") to the users associated with the alert records.
func (am *AlertManager) sendStatusAlert(alertStatus string, systemName string, alertRecord *core.Record) error {
	var emoji string
	if alertStatus == "up" {
		emoji = "\u2705" // Green checkmark emoji
	} else {
		emoji = "\U0001F534" // Red alert emoji
	}

	title := fmt.Sprintf("Connection to %s is %s %v", systemName, alertStatus, emoji)
	message := strings.TrimSuffix(title, emoji)

	if errs := am.app.ExpandRecord(alertRecord, []string{"user"}, nil); len(errs) > 0 {
		return errs["user"]
	}
	user := alertRecord.ExpandedOne("user")
	if user == nil {
		return nil
	}

	return am.sendAlert(AlertMessageData{
		UserID:   user.Id,
		Title:    title,
		Message:  message,
		Link:     am.app.Settings().Meta.AppURL + "/system/" + url.PathEscape(systemName),
		LinkText: "View " + systemName,
	})
}
