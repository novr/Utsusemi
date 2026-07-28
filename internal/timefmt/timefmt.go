package timefmt

import "time"

func Age(d time.Duration) string {
	if d < 0 {
		return "0s"
	}
	d = d.Round(time.Second)
	if d < time.Minute {
		return d.String()
	}
	return d.Round(time.Minute).String()
}

func ExpiresIn(d time.Duration) string {
	if d < 0 {
		return "expired"
	}
	return d.Round(time.Minute).String()
}
