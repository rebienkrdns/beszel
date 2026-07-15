//go:build testing

package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParsePressureLine(t *testing.T) {
	t.Run("parses avg10, avg60, avg300", func(t *testing.T) {
		got := parsePressureLine("avg10=1.23 avg60=4.56 avg300=7.89 total=123456")
		assert.Equal(t, [3]float64{1.23, 4.56, 7.89}, got)
	})

	t.Run("ignores unknown fields", func(t *testing.T) {
		got := parsePressureLine("avg10=0.50 avg60=0.00 avg300=0.00 unknown=1 total=1")
		assert.Equal(t, [3]float64{0.50, 0, 0}, got)
	})

	t.Run("returns zero values for empty input", func(t *testing.T) {
		got := parsePressureLine("")
		assert.Equal(t, [3]float64{}, got)
	})

	t.Run("ignores malformed key=value pairs", func(t *testing.T) {
		got := parsePressureLine("avg10=1.00 malformed avg60=2.00")
		assert.Equal(t, [3]float64{1.00, 2.00, 0}, got)
	})
}
