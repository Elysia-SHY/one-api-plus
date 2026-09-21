package aiproxy

import "github.com/Elysia-SHY/one-api-plus/relay/adaptor/openai"

var ModelList = []string{""}

func init() {
	ModelList = openai.ModelList
}
