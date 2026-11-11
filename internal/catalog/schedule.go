package catalog

import "time"

const secondsPerDay = 24 * 60 * 60

func (c *Catalog) PackOnSale(pack *Pack, now time.Time) bool {
	if pack.Rotation != nil {
		if c.RotationDays <= 0 || pack.Rotation.Count <= 0 {
			return false
		}
		period := now.Unix() / (int64(c.RotationDays) * secondsPerDay)
		return int(period%int64(pack.Rotation.Count)) == pack.Rotation.Index
	}
	if pack.Schedule == nil {
		return true
	}
	date := now.UTC()
	today := int(date.Month())*100 + date.Day()
	start, end := pack.Schedule.StartMonthDay, pack.Schedule.EndMonthDay
	if start > end {
		return today >= start || today <= end
	}
	return today >= start && today <= end
}
