package discover

import (
	"testing"
)

func Test_defaultdiscoverer_Run(_ *testing.T) {
	d := NewDefaultDiscoverer()

	d.Run()
	d.Shutdown()
}
