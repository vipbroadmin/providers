package auth

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
)

func BuildSign(params map[string]string, merchantKey string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	values := url.Values{}
	for _, key := range keys {
		values.Set(key, params[key])
	}

	hashString := values.Encode()
	mac := hmac.New(sha1.New, []byte(merchantKey))
	mac.Write([]byte(hashString))
	return hex.EncodeToString(mac.Sum(nil))
}

func EqualSign(given, expected string) bool {
	return strings.EqualFold(given, expected)
}
