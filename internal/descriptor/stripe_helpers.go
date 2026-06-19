package descriptor

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

func GenerateStripeIdempotencyKey() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("stripe-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}

func FlattenStripeForm(params map[string]any) url.Values {
	values := url.Values{}
	flattenStripeValue(values, "", params)
	return values
}

func ParseStripeError(statusCode int, body []byte) error {
	var decoded struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Code    string `json:"code"`
			Param   string `json:"param"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &decoded) == nil && decoded.Error.Message != "" {
		message := fmt.Sprintf("Stripe error [%s]: %s", decoded.Error.Type, decoded.Error.Message)
		if decoded.Error.Code != "" {
			message += fmt.Sprintf(" (code: %s)", decoded.Error.Code)
		}
		if decoded.Error.Param != "" {
			message += fmt.Sprintf(" (param: %s)", decoded.Error.Param)
		}
		return fmt.Errorf("%s", message)
	}
	return fmt.Errorf("HTTP %d: %s", statusCode, string(body))
}

func flattenStripeValue(values url.Values, prefix string, value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, entry := range typed {
			name := key
			if prefix != "" {
				name = prefix + "[" + key + "]"
			}
			flattenStripeValue(values, name, entry)
		}
	case []any:
		for index, entry := range typed {
			flattenStripeValue(values, fmt.Sprintf("%s[%d]", prefix, index), entry)
		}
	case nil:
	default:
		if text := fmt.Sprint(typed); text != "" && text != "<nil>" {
			values.Set(prefix, text)
		}
	}
}
