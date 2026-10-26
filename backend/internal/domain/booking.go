package domain

import (
	"errors"
	"net/mail"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	FullPrice  = 2000
	ChildPrice = 1000
)

var (
	ErrNotFound = errors.New("checkout not found")
	ErrSoldOut  = errors.New("not enough seats")
	ErrExpired  = errors.New("checkout expired")
)

type ValidationError struct {
	Fields map[string]string `json:"fields"`
}

func (e *ValidationError) Error() string { return "invalid order" }

type ParticipantInput struct {
	Name string `json:"name"`
	Age  *int   `json:"age"`
}
type OrderInput struct {
	Email           string             `json:"email"`
	Participants    []ParticipantInput `json:"participants"`
	SimulateSoldOut bool               `json:"simulate_sold_out"`
}
type Participant struct {
	Name       string `json:"name"`
	Age        int    `json:"age"`
	TicketType string `json:"ticket_type"`
	Price      int    `json:"price"`
}
type Event struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Date        string `json:"date"`
	Venue       string `json:"venue"`
	Remaining   int    `json:"remaining"`
	FullPrice   int    `json:"full_price"`
	ChildPrice  int    `json:"child_price"`
	DemoEnabled bool   `json:"demo_enabled"`
}
type Checkout struct {
	ID           string        `json:"id"`
	Email        string        `json:"email"`
	Participants []Participant `json:"participants"`
	Total        int           `json:"total"`
	ExpiresAt    string        `json:"expires_at"`
}
type Order struct {
	ID           string        `json:"id"`
	Number       string        `json:"number"`
	Email        string        `json:"email"`
	Participants []Participant `json:"participants"`
	Total        int           `json:"total"`
	CreatedAt    string        `json:"created_at"`
}

// PriceOrder owns all ticket and accompaniment rules. Prices are whole rubles.
func PriceOrder(input OrderInput) ([]Participant, int, error) {
	fields := map[string]string{}
	if len(input.Participants) < 1 || len(input.Participants) > 4 {
		fields["participants"] = "В заказе должно быть от 1 до 4 участников."
	}
	email := strings.TrimSpace(input.Email)
	address, err := mail.ParseAddress(email)
	at := strings.LastIndex(email, "@")
	if err != nil || address.Address != email || len(email) > 254 || at < 1 || !strings.Contains(email[at+1:], ".") {
		fields["email"] = "Укажите email в формате name@example.ru."
	}
	priced := make([]Participant, 0, len(input.Participants))
	hasMinor, hasAdult, total := false, false, 0
	for i, p := range input.Participants {
		key := "participants." + strconv.Itoa(i)
		name := strings.TrimSpace(p.Name)
		if name == "" || utf8.RuneCountInString(name) > 80 {
			fields[key+".name"] = "Укажите имя: от 1 до 80 символов."
		}
		if p.Age == nil || *p.Age < 0 || *p.Age > 120 {
			fields[key+".age"] = "Укажите возраст целым числом от 0 до 120 лет."
			continue
		}
		age, price, ticket := *p.Age, FullPrice, "full"
		if age < 14 {
			price, ticket = ChildPrice, "child"
		}
		if age < 18 {
			hasMinor = true
		}
		if age >= 18 {
			hasAdult = true
		}
		priced = append(priced, Participant{Name: name, Age: age, TicketType: ticket, Price: price})
		total += price
	}
	if hasMinor && !hasAdult {
		fields["accompaniment"] = "Участнику младше 18 лет нужен сопровождающий от 18 лет в этом же заказе."
	}
	if len(fields) > 0 {
		return nil, 0, &ValidationError{Fields: fields}
	}
	return priced, total, nil
}
