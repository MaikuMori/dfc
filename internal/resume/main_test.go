package resume

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Setenv(EnvMachineID, "test-machine")
	os.Exit(m.Run())
}
