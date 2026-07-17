//go:build testing

package alerts_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/henrygd/beszel/internal/entities/system"
	beszelTests "github.com/henrygd/beszel/internal/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type systemAlertValueSetter[T any] func(info *system.Info, stats *system.Stats, value T)

type systemAlertTestFixture struct {
	hub     *beszelTests.TestHub
	alertID string
	submit  func(*system.CombinedData) error
}

func createCombinedData[T any](value T, setValue systemAlertValueSetter[T]) *system.CombinedData {
	var data system.CombinedData
	setValue(&data.Info, &data.Stats, value)
	return &data
}

func newSystemAlertTestFixture(t *testing.T, alertName string, min int, threshold float64) *systemAlertTestFixture {
	t.Helper()

	hub, user := beszelTests.GetHubWithUser(t)

	systems, err := beszelTests.CreateSystems(hub, 1, user.Id, "up")
	require.NoError(t, err)
	systemRecord := systems[0]

	sysManagerSystem, err := hub.GetSystemManager().GetSystemFromStore(systemRecord.Id)
	require.NoError(t, err)
	require.NotNil(t, sysManagerSystem)
	sysManagerSystem.StopUpdater()

	userSettings, err := hub.FindFirstRecordByFilter("user_settings", "user={:user}", map[string]any{"user": user.Id})
	require.NoError(t, err)
	userSettings.Set("settings", `{"emails":["test@example.com"],"webhooks":[]}`)
	require.NoError(t, hub.Save(userSettings))

	alertRecord, err := beszelTests.CreateRecord(hub, "alerts", map[string]any{
		"name":   alertName,
		"system": systemRecord.Id,
		"user":   user.Id,
		"min":    min,
		"value":  threshold,
	})
	require.NoError(t, err)

	assert.False(t, alertRecord.GetBool("triggered"), "Alert should not be triggered initially")

	alertsCache := hub.GetAlertManager().GetSystemAlertsCache()
	cachedAlerts := alertsCache.GetAlertsExcludingNames(systemRecord.Id, "Status")
	assert.Len(t, cachedAlerts, 1, "Alert should be in cache")

	return &systemAlertTestFixture{
		hub:     hub,
		alertID: alertRecord.Id,
		submit: func(data *system.CombinedData) error {
			_, err := sysManagerSystem.CreateRecords(data)
			return err
		},
	}
}

func (fixture *systemAlertTestFixture) cleanup() {
	fixture.hub.Cleanup()
}

func submitValue[T any](fixture *systemAlertTestFixture, t *testing.T, value T, setValue systemAlertValueSetter[T]) {
	t.Helper()
	require.NoError(t, fixture.submit(createCombinedData(value, setValue)))
}

func (fixture *systemAlertTestFixture) assertTriggered(t *testing.T, triggered bool, message string) {
	t.Helper()

	alertRecord, err := fixture.hub.FindRecordById("alerts", fixture.alertID)
	require.NoError(t, err)
	assert.Equal(t, triggered, alertRecord.GetBool("triggered"), message)
}

func waitForSystemAlert(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func testOneMinuteSystemAlert[T any](t *testing.T, alertName string, threshold float64, setValue systemAlertValueSetter[T], triggerValue, resolveValue T) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		fixture := newSystemAlertTestFixture(t, alertName, 1, threshold)
		defer fixture.cleanup()

		submitValue(fixture, t, triggerValue, setValue)
		waitForSystemAlert(time.Second)

		fixture.assertTriggered(t, true, "Alert should be triggered")
		assert.Equal(t, 1, fixture.hub.TestMailer.TotalSend(), "An email should have been sent")

		submitValue(fixture, t, resolveValue, setValue)
		waitForSystemAlert(time.Second)

		fixture.assertTriggered(t, false, "Alert should be untriggered")
		assert.Equal(t, 2, fixture.hub.TestMailer.TotalSend(), "A second email should have been sent for untriggering the alert")

		waitForSystemAlert(time.Minute)
	})
}

func testMultiMinuteSystemAlert[T any](t *testing.T, alertName string, threshold float64, min int, setValue systemAlertValueSetter[T], baselineValue, triggerValue, resolveValue T) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		fixture := newSystemAlertTestFixture(t, alertName, min, threshold)
		defer fixture.cleanup()

		submitValue(fixture, t, baselineValue, setValue)
		waitForSystemAlert(time.Minute + time.Second)
		fixture.assertTriggered(t, false, "Alert should not be triggered yet")

		submitValue(fixture, t, triggerValue, setValue)
		waitForSystemAlert(time.Minute)
		fixture.assertTriggered(t, false, "Alert should not be triggered until the history window is full")

		submitValue(fixture, t, triggerValue, setValue)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, true, "Alert should be triggered")
		assert.Equal(t, 1, fixture.hub.TestMailer.TotalSend(), "An email should have been sent")

		submitValue(fixture, t, resolveValue, setValue)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, false, "Alert should be untriggered")
		assert.Equal(t, 2, fixture.hub.TestMailer.TotalSend(), "A second email should have been sent for untriggering the alert")
	})
}

func setCPUAlertValue(info *system.Info, stats *system.Stats, value float64) {
	info.Cpu = value
	stats.Cpu = value
}

func setMemoryAlertValue(info *system.Info, stats *system.Stats, value float64) {
	info.MemPct = value
	stats.MemPct = value
}

func setDiskAlertValue(info *system.Info, stats *system.Stats, value float64) {
	info.DiskPct = value
	stats.DiskPct = value
}

func setBandwidthAlertValue(info *system.Info, stats *system.Stats, value [2]uint64) {
	info.BandwidthBytes = value[0] + value[1]
	stats.Bandwidth = value
}

func megabytesToBytes(mb uint64) uint64 {
	return mb * 1024 * 1024
}

func setGPUAlertValue(info *system.Info, stats *system.Stats, value float64) {
	info.GpuPct = value
	stats.GPUData = map[string]system.GPUData{
		"GPU0": {Usage: value},
	}
}

func setTemperatureAlertValue(info *system.Info, stats *system.Stats, value float64) {
	info.DashboardTemp = value
	stats.Temperatures = map[string]float64{
		"Temp0": value,
	}
}

func setLoadAvgAlertValue(info *system.Info, stats *system.Stats, value [3]float64) {
	info.LoadAvg = value
	stats.LoadAvg = value
}

func setBatteryAlertValue(info *system.Info, stats *system.Stats, value [2]uint8) {
	info.Battery = value
	stats.Battery = value
}

func setMemAvailableAlertValue(info *system.Info, stats *system.Stats, value float64) {
	stats.MemAvailable = value
}

func setOOMKillAlertValue(info *system.Info, stats *system.Stats, value uint32) {
	stats.OOMKillDelta = value
}

func setTCPRetransAlertValue(info *system.Info, stats *system.Stats, value float64) {
	stats.TCPRetransPs = value
}

func setNetworkErrorsAlertValue(info *system.Info, stats *system.Stats, value float64) {
	stats.NetworkErrorsPs = value
}

func TestSystemAlertsOneMin(t *testing.T) {
	testOneMinuteSystemAlert(t, "CPU", 50, setCPUAlertValue, 51, 49)
	testOneMinuteSystemAlert(t, "Memory", 50, setMemoryAlertValue, 51, 49)
	testOneMinuteSystemAlert(t, "Disk", 50, setDiskAlertValue, 51, 49)
	testOneMinuteSystemAlert(t, "Bandwidth", 50, setBandwidthAlertValue, [2]uint64{megabytesToBytes(26), megabytesToBytes(25)}, [2]uint64{megabytesToBytes(25), megabytesToBytes(24)})
	testOneMinuteSystemAlert(t, "GPU", 50, setGPUAlertValue, 51, 49)
	testOneMinuteSystemAlert(t, "Temperature", 70, setTemperatureAlertValue, 71, 69)
	testOneMinuteSystemAlert(t, "LoadAvg1", 4, setLoadAvgAlertValue, [3]float64{4.1, 0, 0}, [3]float64{3.9, 0, 0})
	testOneMinuteSystemAlert(t, "LoadAvg5", 4, setLoadAvgAlertValue, [3]float64{0, 4.1, 0}, [3]float64{0, 3.9, 0})
	testOneMinuteSystemAlert(t, "LoadAvg15", 4, setLoadAvgAlertValue, [3]float64{0, 0, 4.1}, [3]float64{0, 0, 3.9})
	testOneMinuteSystemAlert(t, "Battery", 20, setBatteryAlertValue, [2]uint8{19, 0}, [2]uint8{21, 0})
	testOneMinuteSystemAlert(t, "MemAvailable", 4, setMemAvailableAlertValue, 3.9, 4.1)
	testOneMinuteSystemAlert(t, "OOMKill", 0.5, setOOMKillAlertValue, uint32(1), uint32(0))
	testOneMinuteSystemAlert(t, "TCPRetrans", 10, setTCPRetransAlertValue, 12.0, 8.0)
	testOneMinuteSystemAlert(t, "NetworkErrors", 10, setNetworkErrorsAlertValue, 12.0, 8.0)
}

func TestSystemAlertsTwoMin(t *testing.T) {
	testMultiMinuteSystemAlert(t, "CPU", 50, 2, setCPUAlertValue, 10, 51, 48)
	testMultiMinuteSystemAlert(t, "Memory", 50, 2, setMemoryAlertValue, 10, 51, 48)
	testMultiMinuteSystemAlert(t, "Disk", 50, 2, setDiskAlertValue, 10, 51, 48)
	testMultiMinuteSystemAlert(t, "Bandwidth", 50, 2, setBandwidthAlertValue, [2]uint64{megabytesToBytes(10), megabytesToBytes(10)}, [2]uint64{megabytesToBytes(26), megabytesToBytes(25)}, [2]uint64{megabytesToBytes(10), megabytesToBytes(10)})
	testMultiMinuteSystemAlert(t, "GPU", 50, 2, setGPUAlertValue, 10, 51, 48)
	testMultiMinuteSystemAlert(t, "Temperature", 70, 2, setTemperatureAlertValue, 10, 71, 67)
	testMultiMinuteSystemAlert(t, "LoadAvg1", 4, 2, setLoadAvgAlertValue, [3]float64{0, 0, 0}, [3]float64{4.1, 0, 0}, [3]float64{3.5, 0, 0})
	testMultiMinuteSystemAlert(t, "LoadAvg5", 4, 2, setLoadAvgAlertValue, [3]float64{0, 2, 0}, [3]float64{0, 4.1, 0}, [3]float64{0, 3.5, 0})
	testMultiMinuteSystemAlert(t, "LoadAvg15", 4, 2, setLoadAvgAlertValue, [3]float64{0, 0, 2}, [3]float64{0, 0, 4.1}, [3]float64{0, 0, 3.5})
	testMultiMinuteSystemAlert(t, "Battery", 20, 2, setBatteryAlertValue, [2]uint8{21, 0}, [2]uint8{19, 0}, [2]uint8{25, 1})
	testMultiMinuteSystemAlert(t, "MemAvailable", 4, 2, setMemAvailableAlertValue, 10, 3.9, 4.5)
	testMultiMinuteSystemAlert(t, "TCPRetrans", 10, 2, setTCPRetransAlertValue, 4.0, 12.0, 4.0)
	testMultiMinuteSystemAlert(t, "NetworkErrors", 10, 2, setNetworkErrorsAlertValue, 4.0, 12.0, 4.0)
}

// TestMemAvailableAlertSubjectText guards against a regression where the
// notification subject for "low" alerts (triggered when the value drops
// below the threshold) was computed after the alert name had already been
// rewritten for display (e.g. "MemAvailable" -> "Available Memory"), which
// caused isLowAlert to no longer match and produced an inverted subject.
func TestMemAvailableAlertSubjectText(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newSystemAlertTestFixture(t, "MemAvailable", 1, 4)
		defer fixture.cleanup()

		submitValue(fixture, t, 3.9, setMemAvailableAlertValue)
		waitForSystemAlert(time.Second)

		fixture.assertTriggered(t, true, "Alert should be triggered")
		require.Equal(t, 1, fixture.hub.TestMailer.TotalSend(), "An email should have been sent")
		assert.Contains(t, fixture.hub.TestMailer.LastMessage().Subject, "below threshold",
			"MemAvailable is a low alert; dropping below the threshold should say 'below threshold'")

		submitValue(fixture, t, 4.1, setMemAvailableAlertValue)
		waitForSystemAlert(time.Second)

		fixture.assertTriggered(t, false, "Alert should be untriggered")
		require.Equal(t, 2, fixture.hub.TestMailer.TotalSend(), "A second email should have been sent for untriggering the alert")
		assert.Contains(t, fixture.hub.TestMailer.LastMessage().Subject, "above threshold",
			"resolving a low alert should say 'above threshold'")

		waitForSystemAlert(time.Minute)
	})
}

// TestOOMKillAlertSubjectText guards against the generic "above/below
// threshold" and "averaged X for Y minutes" wording being used for OOMKill,
// which is an event counter, not a continuous value - the event-style
// override in sendSystemAlert must actually take effect.
func TestOOMKillAlertSubjectText(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newSystemAlertTestFixture(t, "OOMKill", 1, 0.5)
		defer fixture.cleanup()

		submitValue(fixture, t, uint32(1), setOOMKillAlertValue)
		waitForSystemAlert(time.Second)

		fixture.assertTriggered(t, true, "Alert should be triggered")
		require.Equal(t, 1, fixture.hub.TestMailer.TotalSend(), "An email should have been sent")
		assert.Contains(t, fixture.hub.TestMailer.LastMessage().Subject, "OOM Killer event detected",
			"OOMKill triggering should use event-style wording, not 'above threshold'")

		submitValue(fixture, t, uint32(0), setOOMKillAlertValue)
		waitForSystemAlert(time.Second)

		fixture.assertTriggered(t, false, "Alert should be untriggered")
		require.Equal(t, 2, fixture.hub.TestMailer.TotalSend(), "A second email should have been sent for untriggering the alert")
		assert.Contains(t, fixture.hub.TestMailer.LastMessage().Subject, "OOM Killer events cleared",
			"OOMKill resolving should use event-style wording, not 'below threshold'")

		waitForSystemAlert(time.Minute)
	})
}

// TestOOMKillWindowedAlertSumsNotAverages demonstrates the actual value of
// the sum-not-average fix for OOMKill's finalization case in
// HandleSystemAlerts: a single real kill must trigger a windowed
// (multi-minute) alert, which is the feature's primary real-world use case
// and precisely the scenario that was broken before the fix (the UI's
// alert-creation form defaults new alerts to a 10-minute window, and the
// averaging previously applied to every alert type diluted a lone kill's
// delta=1 below the default 0.5 threshold once divided by ~10 historical
// records).
//
// This test uses min=2 (a 2-minute window, the smallest window that still
// exercises the windowed/summed code path - min=1 alerts bypass it via the
// separate "instant check" path) and threshold=0.5, matching lib/alerts.ts's
// OOMKill.start default, so it exercises the real production configuration
// rather than an artificially chosen threshold.
//
// Timing derivation (verified empirically against the actual finalization
// logic - see task report for the debug-logged run): HandleSystemAlerts
// requires a historical record older than the alert's window
// (now-min*60s) to exist before it will evaluate the alert at all (the
// "oldestRecordTime" gate), and separately requires alert.count (the
// number of historical records that fall inside the window) to be >=
// min/1.2 (~1.67 for min=2, i.e. >= 2) before it will trigger or resolve.
// A historical record is excluded from the window once
// record.Created-10s is older than now-min*60s.
//
//   - t=0s: anchor record (delta=0) - establishes history predating the
//     window so the gate can eventually pass.
//   - t=121s: a second delta=0 record. Alone with the anchor this gives
//     count=1 (below the count>=2 gate), so no evaluation fires yet.
//   - t=122s: the kill (delta=1). Now count=2 (the t=121 and t=122
//     records both fall inside the window). With the fix, alert.val is the
//     raw sum (0+1=1), which is > 0.5, so the alert triggers. Under the old
//     averaging behavior this would have been (0+1)/2=0.5, which is NOT >
//     0.5 - the alert would NOT have triggered. This is the exact defect
//     this test guards against.
//   - t=183s (61s later): a fresh delta=0 record. The kill (t=122) is
//     still inside the 2-minute window, so the alert must still be
//     triggered (sum=0+1+0=1).
//   - t=244s (61s later still): another fresh delta=0 record. By now the
//     kill (created at t=122) has aged out of the window (t=122-10=112s
//     is older than the window start of t=244-120=124s), leaving only
//     zero-delta records in the window, so the alert resolves.
func TestOOMKillWindowedAlertSumsNotAverages(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newSystemAlertTestFixture(t, "OOMKill", 2, 0.5)
		defer fixture.cleanup()

		submitValue(fixture, t, uint32(0), setOOMKillAlertValue)
		waitForSystemAlert(121 * time.Second)

		submitValue(fixture, t, uint32(0), setOOMKillAlertValue)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, false, "Alert should not be triggered yet (below the minCount gate)")

		submitValue(fixture, t, uint32(1), setOOMKillAlertValue)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, true, "A single kill must trigger a windowed alert (sum, not average)")
		assert.Equal(t, 1, fixture.hub.TestMailer.TotalSend(), "An email should have been sent")

		waitForSystemAlert(60 * time.Second)
		submitValue(fixture, t, uint32(0), setOOMKillAlertValue)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, true, "Alert should still be triggered - the kill hasn't aged out of the window yet")

		waitForSystemAlert(60 * time.Second)
		submitValue(fixture, t, uint32(0), setOOMKillAlertValue)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, false, "Alert should resolve once the kill ages out of the window")
		assert.Equal(t, 2, fixture.hub.TestMailer.TotalSend(), "A second email should have been sent for untriggering the alert")
	})
}
