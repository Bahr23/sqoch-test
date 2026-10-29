package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type EligibilityClient struct {
	BaseURL, Token string
	Client         *http.Client
}

func (c *EligibilityClient) Eligible(ctx context.Context, id string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(c.BaseURL, "/")+"/internal/notifications/"+id+"/eligibility", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	response, err := c.Client.Do(req)
	if err != nil {
		return false, fmt.Errorf("не удалось проверить действующую покупку")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return false, fmt.Errorf("проверка покупки временно недоступна (%d)", response.StatusCode)
	}
	var v struct {
		Eligible *bool `json:"eligible"`
	}
	if json.NewDecoder(http.MaxBytesReader(nil, response.Body, 4096)).Decode(&v) != nil || v.Eligible == nil {
		return false, fmt.Errorf("некорректный ответ проверки покупки")
	}
	return *v.Eligible, nil
}
