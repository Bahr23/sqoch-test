package domain

import (
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// EventInput contains only editable fields. UUID, remaining and sold are server-owned.
type EventInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Tagline     string `json:"tagline"`
	CoverURL    string `json:"cover_url"`
	Date        string `json:"date"`
	Timezone    string `json:"timezone"`
	Venue       string `json:"venue"`
	FullPrice   int    `json:"full_price"`
	ChildPrice  int    `json:"child_price"`
	Capacity    int    `json:"capacity"`
}

func (v *EventInput) Validate() error {
	fields := map[string]string{}
	for _, f := range []struct {
		key      string
		value    *string
		min, max int
	}{
		{"title", &v.Title, 1, 160}, {"description", &v.Description, 1, 10000}, {"category", &v.Category, 1, 100},
		{"tagline", &v.Tagline, 0, 120}, {"venue", &v.Venue, 1, 300}, {"timezone", &v.Timezone, 1, 80},
	} {
		*f.value = strings.TrimSpace(*f.value)
		if n := utf8.RuneCountInString(*f.value); n < f.min || n > f.max {
			fields[f.key] = "Проверьте длину и заполнение поля."
		}
	}
	if _, err := time.Parse(time.RFC3339, v.Date); err != nil {
		fields["date"] = "Укажите дату и время с часовым поясом."
	}
	if _, err := time.LoadLocation(v.Timezone); err != nil || v.Timezone == "Local" {
		fields["timezone"] = "Укажите часовой пояс IANA, например Europe/Moscow."
	}
	if v.Capacity < 1 || v.Capacity > 1000000 {
		fields["capacity"] = "Вместимость: от 1 до 1 000 000 мест."
	}
	if v.FullPrice < 1 || v.FullPrice > 1000000 {
		fields["full_price"] = "Цена: от 1 до 1 000 000 ₽."
	}
	if v.ChildPrice < 1 || v.ChildPrice > 1000000 {
		fields["child_price"] = "Цена: от 1 до 1 000 000 ₽."
	}
	v.CoverURL = strings.TrimSpace(v.CoverURL)
	if v.CoverURL != "" {
		u, err := url.Parse(v.CoverURL)
		if err != nil || len(v.CoverURL) > 2048 || u.User != nil || u.Host == "" || u.Scheme != "https" {
			fields["cover_url"] = "Используйте полный HTTPS-адрес изображения."
		}
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}
func (e Event) Input() EventInput {
	return EventInput{Title: e.Title, Description: e.Description, Category: e.Category, Tagline: e.Tagline, CoverURL: e.CoverURL, Date: e.Date, Timezone: e.Timezone, Venue: e.Venue, FullPrice: e.FullPrice, ChildPrice: e.ChildPrice, Capacity: e.Capacity}
}
