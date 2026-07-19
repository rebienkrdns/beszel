package alerts

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/henrygd/beszel/internal/entities/container"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// containerAlertNames is the set of alert names handled by HandleContainerAlerts.
var containerAlertNames = map[string]struct{}{
	"ContainerCPU":      {},
	"ContainerMem":      {},
	"ContainerNet":      {},
	"ContainerRestarts": {},
	"ContainerOOM":      {},
}

func isContainerAlertName(name string) bool {
	_, ok := containerAlertNames[name]
	return ok
}

// containerAlertStats holds the fields needed for unmarshaling from container_stats records.
type containerAlertStats struct {
	Name         string    `json:"n"`
	Cpu          float64   `json:"c"`
	Mem          float64   `json:"m"`
	Bandwidth    [2]uint64 `json:"b"`
	NetworkSent  float64   `json:"ns"`
	NetworkRecv  float64   `json:"nr"`
	Restarts     uint8     `json:"rs"`
	OomKillDelta uint16    `json:"ok"`
}

// containerAlertData holds the state needed to evaluate a single container alert.
type containerAlertData struct {
	systemRecord    *core.Record
	alertData       CachedAlertData
	name            string
	unit            string
	val             float64
	threshold       float64
	triggered       bool
	time            time.Time
	count           uint8
	min             uint8
	containerSums   map[string]float64
	containerCounts map[string]uint8
	maxContainer    string // name of the container with the highest averaged value
}

// HandleContainerAlerts evaluates per-system container metric alerts using the current
// snapshot of container stats (for min==1) or historical container_stats records (for min>1).
func (am *AlertManager) HandleContainerAlerts(systemRecord *core.Record, containers []*container.Stats) error {
	allAlerts := am.alertsCache.GetSystemAlerts(systemRecord.Id)

	var validAlerts []containerAlertData
	now := systemRecord.GetDateTime("updated").Time().UTC()
	oldestTime := now

	for _, alertData := range allAlerts {
		name := alertData.Name
		if !isContainerAlertName(name) {
			continue
		}

		// Find the max value across all current containers.
		var val float64
		var maxContainerName string
		var unit string

		switch name {
		case "ContainerCPU":
			unit = "%"
			for _, ctr := range containers {
				if ctr.Cpu > val {
					val = ctr.Cpu
					maxContainerName = ctr.Name
				}
			}
		case "ContainerMem":
			unit = " MB"
			for _, ctr := range containers {
				if ctr.Mem > val {
					val = ctr.Mem
					maxContainerName = ctr.Name
				}
			}
		case "ContainerNet":
			unit = " MB/s"
			for _, ctr := range containers {
				bw := float64(ctr.Bandwidth[0]+ctr.Bandwidth[1]) / (1024 * 1024)
				if bw == 0 {
					bw = ctr.NetworkSent + ctr.NetworkRecv
				}
				if bw > val {
					val = bw
					maxContainerName = ctr.Name
				}
			}
		case "ContainerRestarts":
			unit = ""
			for _, ctr := range containers {
				v := float64(ctr.Restarts)
				if v > val {
					val = v
					maxContainerName = ctr.Name
				}
			}
		case "ContainerOOM":
			unit = ""
			for _, ctr := range containers {
				v := float64(ctr.OomKillDelta)
				if v > val {
					val = v
					maxContainerName = ctr.Name
				}
			}
		default:
			continue
		}

		triggered := alertData.Triggered
		threshold := alertData.Value

		if (!triggered && val <= threshold) || (triggered && val > threshold) {
			continue
		}

		min := max(1, alertData.Min)

		alert := containerAlertData{
			systemRecord: systemRecord,
			alertData:    alertData,
			name:         name,
			unit:         unit,
			val:          val,
			threshold:    threshold,
			triggered:    triggered,
			min:          min,
			maxContainer: maxContainerName,
		}

		if min == 1 {
			alert.triggered = val > threshold
			go am.sendContainerAlert(alert)
			continue
		}

		alert.time = now.Add(-time.Duration(min) * time.Minute)
		if alert.time.Before(oldestTime) {
			oldestTime = alert.time
		}
		alert.containerSums = make(map[string]float64)
		alert.containerCounts = make(map[string]uint8)
		validAlerts = append(validAlerts, alert)
	}

	if len(validAlerts) == 0 {
		return nil
	}

	// Query historical container_stats records for windowed evaluation.
	containerStats := []struct {
		Stats   []byte         `db:"stats"`
		Created types.DateTime `db:"created"`
	}{}

	err := am.hub.DB().
		Select("stats", "created").
		From("container_stats").
		Where(dbx.NewExp(
			"system={:system} AND type='1m' AND created > {:created}",
			dbx.Params{
				"system":  systemRecord.Id,
				"created": oldestTime.Add(-time.Second * 90),
			},
		)).
		OrderBy("created").
		All(&containerStats)
	if err != nil || len(containerStats) == 0 {
		return err
	}

	oldestRecordTime := containerStats[0].Created.Time()

	filteredAlerts := make([]containerAlertData, 0, len(validAlerts))
	for _, alert := range validAlerts {
		if alert.time.After(oldestRecordTime) {
			filteredAlerts = append(filteredAlerts, alert)
		}
	}
	validAlerts = filteredAlerts

	if len(validAlerts) == 0 {
		return nil
	}

	for i := range containerStats {
		stat := containerStats[i]
		var statsArray []containerAlertStats
		if err := json.Unmarshal(stat.Stats, &statsArray); err != nil {
			return err
		}
		statCreated := stat.Created.Time().Add(-time.Second * 10)

		for j := range validAlerts {
			alert := &validAlerts[j]
			if i == 0 {
				alert.containerSums = make(map[string]float64)
				alert.containerCounts = make(map[string]uint8)
				alert.count = 0
			}
			if statCreated.Before(alert.time) {
				continue
			}
			alert.count++
			for _, ctr := range statsArray {
				var v float64
				switch alert.name {
				case "ContainerCPU":
					v = ctr.Cpu
				case "ContainerMem":
					v = ctr.Mem
				case "ContainerNet":
					bw := float64(ctr.Bandwidth[0]+ctr.Bandwidth[1]) / (1024 * 1024)
					if bw == 0 {
						bw = ctr.NetworkSent + ctr.NetworkRecv
					}
					v = bw
				case "ContainerRestarts":
					v = float64(ctr.Restarts)
				case "ContainerOOM":
					v = float64(ctr.OomKillDelta)
				}
				alert.containerSums[ctr.Name] += v
				alert.containerCounts[ctr.Name]++
			}
		}
	}

	// isEventCounter returns true for alert types that are per-poll event counts
	// (should be summed, not averaged, across the window).
	isEventCounter := func(name string) bool {
		return name == "ContainerRestarts" || name == "ContainerOOM"
	}

	for i := range validAlerts {
		alert := &validAlerts[i]
		maxVal := 0.0
		maxName := ""
		for ctrName, sum := range alert.containerSums {
			count := alert.containerCounts[ctrName]
			if count == 0 {
				continue
			}
			var v float64
			if isEventCounter(alert.name) {
				v = sum // total events in window, not average
			} else {
				v = sum / float64(count)
			}
			if v > maxVal {
				maxVal = v
				maxName = ctrName
			}
		}
		alert.val = maxVal
		alert.maxContainer = maxName

		minCount := float32(alert.min) / 1.2
		if float32(alert.count) >= minCount {
			if !alert.triggered && alert.val > alert.threshold {
				alert.triggered = true
				go am.sendContainerAlert(*alert)
			} else if alert.triggered && alert.val <= alert.threshold {
				alert.triggered = false
				go am.sendContainerAlert(*alert)
			}
		}
	}
	return nil
}

func (am *AlertManager) sendContainerAlert(alert containerAlertData) {
	systemName := alert.systemRecord.GetString("name")

	containerDesc := "container"
	if alert.maxContainer != "" {
		containerDesc = alert.maxContainer
	}

	minutesLabel := "minute"
	if alert.min > 1 {
		minutesLabel += "s"
	}

	var subject, body string

	switch alert.name {
	case "ContainerRestarts":
		if alert.triggered {
			subject = fmt.Sprintf("%s %s restarted", systemName, containerDesc)
			body = fmt.Sprintf("%s restarted %g time(s) in the previous %v %s.",
				containerDesc, alert.val, alert.min, minutesLabel)
		} else {
			subject = fmt.Sprintf("%s %s restarts cleared", systemName, containerDesc)
			body = fmt.Sprintf("No new restarts for %s on %s in the previous %v %s.",
				containerDesc, systemName, alert.min, minutesLabel)
		}
	case "ContainerOOM":
		if alert.triggered {
			subject = fmt.Sprintf("%s %s OOM kill detected", systemName, containerDesc)
			body = fmt.Sprintf("The kernel OOM killer terminated a process in container %s on %s.",
				containerDesc, systemName)
		} else {
			subject = fmt.Sprintf("%s %s OOM kills cleared", systemName, containerDesc)
			body = fmt.Sprintf("No new OOM kills in container %s on %s in the previous %v %s.",
				containerDesc, systemName, alert.min, minutesLabel)
		}
	default:
		var metricLabel string
		switch alert.name {
		case "ContainerCPU":
			metricLabel = "CPU"
		case "ContainerMem":
			metricLabel = "memory"
		case "ContainerNet":
			metricLabel = "bandwidth"
		default:
			metricLabel = alert.name
		}
		if alert.triggered {
			subject = fmt.Sprintf("%s %s %s above threshold", systemName, containerDesc, metricLabel)
		} else {
			subject = fmt.Sprintf("%s %s %s below threshold", systemName, containerDesc, metricLabel)
		}
		body = fmt.Sprintf("%s %s averaged %.2f%s for the previous %v %s.",
			systemName, containerDesc, alert.val, alert.unit, alert.min, minutesLabel)
	}

	if err := am.setAlertTriggered(alert.alertData, alert.triggered); err != nil {
		return
	}
	am.SendAlert(AlertMessageData{
		UserID:   alert.alertData.UserID,
		SystemID: alert.systemRecord.Id,
		Title:    subject,
		Message:  body,
		Link:     am.hub.MakeLink("system", alert.systemRecord.Id),
		LinkText: "View " + systemName,
	})
}
