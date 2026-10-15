package auth

import "os"

func Environment() Config {
	get := func(name, fallback string) string {
		if value := os.Getenv(name); value != "" {
			return value
		}
		return fallback
	}
	return Config{
		Site: get("KOUJOU_SITE_URL", "https://localhost:1420"), API: get("KOUJOU_PUBLIC_API_URL", "https://localhost:8080"),
		Issuer: get("KOUJOU_OIDC_ISSUER", "http://127.0.0.1:4444/"), HydraAdmin: get("KOUJOU_HYDRA_ADMIN_URL", "http://127.0.0.1:4445"),
		KratosPublic: get("KOUJOU_KRATOS_PUBLIC_URL", "http://127.0.0.1:4433"), KratosAdmin: get("KOUJOU_KRATOS_ADMIN_URL", "http://127.0.0.1:4434"),
		WebClient: get("KOUJOU_WEB_CLIENT_ID", "koujou-web"), WebSecret: os.Getenv("KOUJOU_WEB_CLIENT_SECRET"), NativeClient: get("KOUJOU_NATIVE_CLIENT_ID", "koujou-native"), NativeRedirect: get("KOUJOU_NATIVE_REDIRECT_URI", "http://127.0.0.1:19487/callback"), Audience: get("KOUJOU_API_AUDIENCE", "koujou-api"), Key: []byte(os.Getenv("KOUJOU_AUTH_KEY")),
	}
}
