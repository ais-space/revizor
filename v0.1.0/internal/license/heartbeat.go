package license

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// HeartbeatClient — HTTP-клиент для отправки heartbeat-запросов на сервер лицензирования.
type HeartbeatClient struct {
	ServerURL  string
	HTTPClient *http.Client
}

// HeartbeatRequest — тело запроса к серверу лицензирования.
type HeartbeatRequest struct {
	KID       string `json:"kid"`
	IID       string `json:"iid"`
	MachineID string `json:"machine_id"`
	Version   string `json:"version"`
}

// HeartbeatResponse — ответ сервера лицензирования.
type HeartbeatResponse struct {
	ActivationToken string `json:"activation_token"`
	ExpiresIn       int    `json:"expires_in"` // секунд до истечения токена
}

// NewHeartbeatClient создаёт новый HeartbeatClient.
func NewHeartbeatClient(serverURL string) *HeartbeatClient {
	return &HeartbeatClient{
		ServerURL: serverURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ActivationRequest — тело запроса активации.
type ActivationRequest struct {
	KID       string `json:"kid"`
	MachineID string `json:"machine_id"`
	Version   string `json:"version"`
}

// ActivationResponse — ответ сервера на активацию.
type ActivationResponse struct {
	IID             string `json:"iid"`
	ActivationToken string `json:"activation_token"`
	ExpiresIn       int    `json:"expires_in"`
	MachinesCurrent int    `json:"machines_current"`
	MachinesMax     *int   `json:"machines_max"`  // nil = безлимит
	// v56: поля возвращаемые сервером после активации
	Iat         int    `json:"iat"`          // effective iat
	Exp         int    `json:"exp"`          // effective exp
	RenewalType string `json:"renewal_type"` // "new" | "renew" | ""
}

// PingServer проверяет доступность сервера лицензирования.
// GET /api/v1/license/ping → 200 OK
func (c *HeartbeatClient) PingServer(ctx context.Context) error {
	url := c.ServerURL + "/api/v1/license/ping"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create ping request: %w", err)
	}

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ping request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// SendActivation отправляет запрос активации на сервер.
// POST /api/v1/license/activate
func (c *HeartbeatClient) SendActivation(ctx context.Context, req ActivationRequest) (*ActivationResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal activation request: %w", err)
	}

	url := c.ServerURL + "/api/v1/license/activate"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create activation request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("activation request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		// Читаем тело чтобы понять причину
		var errBody struct {
			Detail string `json:"detail"`
		}
		json.NewDecoder(resp.Body).Decode(&errBody)
		if errBody.Detail == "license_revoked" {
			return nil, ErrLicenseRevoked
		}
		if errBody.Detail != "" {
			return nil, fmt.Errorf("activation rejected: %s", errBody.Detail)
		}
		return nil, fmt.Errorf("activation rejected by server (403)")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("activation returned HTTP %d", resp.StatusCode)
	}

	var ar ActivationResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return nil, fmt.Errorf("decode activation response: %w", err)
	}
	return &ar, nil
}

// SendHeartbeat отправляет heartbeat-запрос с ретраями (exponential backoff).
// Возвращает активационный токен или ошибку после исчерпания попыток.
func (c *HeartbeatClient) SendHeartbeat(ctx context.Context, req HeartbeatRequest) (*HeartbeatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal heartbeat request: %w", err)
	}

	url := c.ServerURL + "/api/v1/license/heartbeat"

	// Ретраи: 3 попытки с exponential backoff (1s, 2s, 4s)
	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			lastErr = fmt.Errorf("create request: %w", err)
			continue
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := c.HTTPClient.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("heartbeat request: %w", err)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			var hr HeartbeatResponse
			if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
				resp.Body.Close()
				lastErr = fmt.Errorf("decode heartbeat response: %w", err)
				continue
			}
			resp.Body.Close()
			return &hr, nil
		}

		resp.Body.Close()

		if resp.StatusCode == http.StatusForbidden {
			var errBody struct {
				Detail string `json:"detail"`
			}
			json.NewDecoder(resp.Body).Decode(&errBody)
			if errBody.Detail == "license_revoked" {
				return nil, ErrLicenseRevoked
			}
			return nil, fmt.Errorf("license rejected by server (403)")
		}

		lastErr = fmt.Errorf("heartbeat returned HTTP %d", resp.StatusCode)
	}

	return nil, fmt.Errorf("heartbeat failed after %d attempts: %w", maxRetries, lastErr)
}

// ── v55: Алерты о подозрительной активности ────────────────────────────────

// AlertRequest — тело запроса алерта от бинарника к серверу (v55, Фаза 4).
type AlertRequest struct {
	KID          string                 `json:"kid"`
	IID          string                 `json:"iid"`
	AlertType    string                 `json:"alert_type"`
	Severity     string                 `json:"severity"`
	DetailsJSON  map[string]interface{} `json:"details_json"`
}

// ── v56: Сдвиг начала срока лицензии ────────────────────────────────────

// ShiftIatResponse — ответ сервера на сдвиг iat.
type ShiftIatResponse struct {
	Iat    int  `json:"iat"`
	Exp    int  `json:"exp"`
	Shifted bool `json:"shifted"`
}

// ShiftIat отправляет запрос на сдвиг iat на now.
func (c *HeartbeatClient) ShiftIat(ctx context.Context, kid, iid string) (*ShiftIatResponse, error) {
	reqBody, _ := json.Marshal(map[string]string{"kid": kid, "iid": iid})
	url := c.ServerURL + "/api/v1/license/shift-iat"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("shift-iat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("shift-iat request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("shift-iat returned HTTP %d", resp.StatusCode)
	}

	var result ShiftIatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("shift-iat decode: %w", err)
	}
	return &result, nil
}

// SendAlert отправляет alert о подозрительной активности на сервер (fire-and-forget).
func (c *HeartbeatClient) SendAlert(ctx context.Context, alert AlertRequest) error {
	body, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("marshal alert request: %w", err)
	}

	url := c.ServerURL + "/api/v1/admin/licenses/alerts"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("alert request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("alert request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("alert returned HTTP %d", resp.StatusCode)
	}
	return nil
}
