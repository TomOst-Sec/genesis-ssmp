package god

import "time"

var nowFunc = func() time.Time { return time.Now() }
