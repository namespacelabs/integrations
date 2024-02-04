package apienv

import "os"

func IAMEndpoint() string {
	if v := os.Getenv("NSC_IAM_ENDPOINT"); v != "" {
		return v
	}

	return GlobalEndpoint()
}

func GlobalEndpoint() string {
	if v := os.Getenv("NSC_GLOBAL_ENDPOINT"); v != "" {
		return v
	}

	return "https://api.namespacelabs.net"
}
