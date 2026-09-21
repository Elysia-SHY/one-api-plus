package relay

import (
	. "github.com/smartystreets/goconvey/convey"
	"github.com/Elysia-SHY/one-api-plus/relay/apitype"
	"testing"
)

func TestGetAdaptor(t *testing.T) {
	Convey("get adaptor", t, func() {
		for i := 0; i < apitype.Dummy; i++ {
			a := GetAdaptor(i)
			So(a, ShouldNotBeNil)
		}
	})
}
