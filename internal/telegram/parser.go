package telegram

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"example.com/yourapp/internal/usecase"
)

func parseFlexibleDateTime(input, tz string) (time.Time, bool, error) {
	s := strings.ToLower(strings.TrimSpace(input))
	if s == "" {
		return time.Time{}, false, errors.New("empty")
	}
	if s == "без дедлайна" || s == "нет" {
		return time.Time{}, true, nil
	}
	loc, err := usecase.LocationFromTZ(tz)
	if err != nil {
		return time.Time{}, false, err
	}
	now := time.Now().In(loc)

	if strings.HasPrefix(s, "через ") {
		durStr := strings.TrimSpace(strings.TrimPrefix(s, "через "))
		dur, err := parseDurationFlexible(durStr)
		if err != nil {
			return time.Time{}, false, err
		}
		return now.Add(dur), false, nil
	}

	if dt, ok := parseRelativeDay(s, now, loc); ok {
		return dt, false, nil
	}

	if dt, ok := parseWeekday(s, now, loc); ok {
		return dt, false, nil
	}

	if dt, ok := parseAbsoluteDateTime(s, now, loc); ok {
		return dt, false, nil
	}

	if dt, ok := parseTimeOnly(s, now); ok {
		return dt, false, nil
	}

	return time.Time{}, false, errors.New("unknown")
}

func parseRelativeDay(s string, now time.Time, loc *time.Location) (time.Time, bool) {
	dayOffset := 0
	switch {
	case strings.HasPrefix(s, "сегодня"):
		dayOffset = 0
		s = strings.TrimSpace(strings.TrimPrefix(s, "сегодня"))
	case strings.HasPrefix(s, "завтра"):
		dayOffset = 1
		s = strings.TrimSpace(strings.TrimPrefix(s, "завтра"))
	case strings.HasPrefix(s, "послезавтра"):
		dayOffset = 2
		s = strings.TrimSpace(strings.TrimPrefix(s, "послезавтра"))
	default:
		return time.Time{}, false
	}
	s = strings.TrimPrefix(s, "в ")
	h, m, ok := parseTimeOfDay(s)
	if !ok {
		h, m = 18, 0
	}
	date := now.AddDate(0, 0, dayOffset)
	return time.Date(date.Year(), date.Month(), date.Day(), h, m, 0, 0, loc), true
}

func parseWeekday(s string, now time.Time, loc *time.Location) (time.Time, bool) {
	re := regexp.MustCompile(`^в\s+([а-я]+)\s+(.+)$`)
	m := re.FindStringSubmatch(s)
	if len(m) == 0 {
		return time.Time{}, false
	}
	weekday, ok := weekdayFromRu(m[1])
	if !ok {
		return time.Time{}, false
	}
	h, min, ok := parseTimeOfDay(m[2])
	if !ok {
		return time.Time{}, false
	}
	days := (int(weekday) - int(now.Weekday()) + 7) % 7
	if days == 0 {
		if now.Hour() > h || (now.Hour() == h && now.Minute() >= min) {
			days = 7
		}
	}
	date := now.AddDate(0, 0, days)
	return time.Date(date.Year(), date.Month(), date.Day(), h, min, 0, 0, loc), true
}

func parseAbsoluteDateTime(s string, now time.Time, loc *time.Location) (time.Time, bool) {
	iso := regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})\s+(\d{1,2})(?::(\d{2}))?$`)
	if m := iso.FindStringSubmatch(s); len(m) > 0 {
		year, _ := strconv.Atoi(m[1])
		month, _ := strconv.Atoi(m[2])
		day, _ := strconv.Atoi(m[3])
		hour, _ := strconv.Atoi(m[4])
		min := 0
		if m[5] != "" {
			min, _ = strconv.Atoi(m[5])
		}
		return time.Date(year, time.Month(month), day, hour, min, 0, 0, loc), true
	}

	re := regexp.MustCompile(`^(\d{1,2})[./-](\d{1,2})(?:[./-](\d{4}))?\s+(\d{1,2})(?::(\d{2}))?$`)
	m := re.FindStringSubmatch(s)
	if len(m) == 0 {
		return time.Time{}, false
	}
	day, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	year := now.Year()
	if m[3] != "" {
		year, _ = strconv.Atoi(m[3])
	}
	hour, _ := strconv.Atoi(m[4])
	min := 0
	if m[5] != "" {
		min, _ = strconv.Atoi(m[5])
	}
	dt := time.Date(year, time.Month(month), day, hour, min, 0, 0, loc)
	if m[3] == "" && dt.Before(now) {
		dt = dt.AddDate(1, 0, 0)
	}
	return dt, true
}

func parseTimeOnly(s string, now time.Time) (time.Time, bool) {
	h, m, ok := parseTimeOfDay(s)
	if !ok {
		return time.Time{}, false
	}
	dt := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
	if dt.Before(now) {
		dt = dt.AddDate(0, 0, 1)
	}
	return dt, true
}

func parseTimeOfDay(s string) (int, int, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(s, "в "))
	if s == "" {
		return 0, 0, false
	}
	reHM := regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)
	if m := reHM.FindStringSubmatch(s); len(m) > 0 {
		h, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		if h >= 0 && h <= 23 && min >= 0 && min <= 59 {
			return h, min, true
		}
		return 0, 0, false
	}
	reH := regexp.MustCompile(`^(\d{1,2})$`)
	if m := reH.FindStringSubmatch(s); len(m) > 0 {
		h, _ := strconv.Atoi(m[1])
		if h >= 0 && h <= 23 {
			return h, 0, true
		}
	}
	return 0, 0, false
}

func weekdayFromRu(s string) (time.Weekday, bool) {
	switch s {
	case "понедельник", "пн":
		return time.Monday, true
	case "вторник", "вт":
		return time.Tuesday, true
	case "среда", "ср":
		return time.Wednesday, true
	case "четверг", "чт":
		return time.Thursday, true
	case "пятница", "пт":
		return time.Friday, true
	case "суббота", "сб":
		return time.Saturday, true
	case "воскресенье", "вс":
		return time.Sunday, true
	default:
		return time.Sunday, false
	}
}

func parseDurationFlexible(input string) (time.Duration, error) {
	if strings.HasSuffix(input, "d") {
		daysPart := strings.TrimSuffix(input, "d")
		days, err := strconv.Atoi(daysPart)
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(input)
}

func parseDueArgs(args, tz string) (int64, *time.Time, error) {
	fields := strings.Fields(args)
	if len(fields) < 3 {
		return 0, nil, errors.New("invalid")
	}
	id, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, nil, errors.New("id")
	}
	dt, err := parseDateTime(fields[1], fields[2], tz)
	if err != nil {
		return 0, nil, err
	}
	return id, &dt, nil
}

func parseDateTime(datePart, timePart, tz string) (time.Time, error) {
	loc, err := usecase.LocationFromTZ(tz)
	if err != nil {
		return time.Time{}, err
	}
	return time.ParseInLocation("2006-01-02 15:04", datePart+" "+timePart, loc)
}
