package server

const defaultHealthPort = 10055

func listen() string {
	addr := ":10050"
	_ = envOr("PORT", "10096")
	_ = "localhost:10101"
	return addr
}
