package middleware

import (
	"net/http"
	"net/url"
)

// CORS 开发期跨域：允许 localhost / 127.0.0.1 / [::1] 任意端口，
// 以及 Capacitor WebView 的 capacitor://localhost、ionic://localhost。
// 上线前收紧为白名单（主线程决策）。
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && devOriginAllowed(origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Max-Age", "600")
		}
		// 预检请求直接短路（无论 origin 是否放行，都不进业务链）。
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func devOriginAllowed(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "http", "https":
		host := u.Hostname()
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	case "capacitor", "ionic":
		return u.Host == "localhost"
	default:
		return false
	}
}
