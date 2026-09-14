package telegram

import (
	"os"
	"testing"

	"opsplatform-alert-backend/timezone"
)

// Message footers render in the platform's display timezone, which is unset by
// default and therefore follows whatever zone the machine running the tests is
// in. Pin it so the expected strings below are the same everywhere — on a
// laptop in +08 and on a CI runner in UTC.
func TestMain(m *testing.M) {
	if err := timezone.Set("UTC"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
