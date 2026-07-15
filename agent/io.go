package agent

// getIOPressure reads Linux PSI (Pressure Stall Information) for I/O from
// /proc/pressure/io and returns the "some" and "full" stall percentages
// [avg10, avg60, avg300] each. Returns zero arrays on non-Linux systems or if
// the file is unavailable.
func getIOPressure() (some [3]float64, full [3]float64) {
	return readPressureFile("/proc/pressure/io")
}
