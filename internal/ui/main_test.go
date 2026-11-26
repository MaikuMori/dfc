package ui

import (
	"os"
	"testing"

	"github.com/MaikuMori/dfc/internal/resume"
)

func TestMain(m *testing.M) {
	_ = os.Setenv(resume.EnvMachineID, "test-machine")
	os.Exit(m.Run())
}
