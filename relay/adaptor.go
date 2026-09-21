package relay

import (
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/aiproxy"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/ali"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/anthropic"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/aws"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/baidu"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/cloudflare"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/cohere"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/coze"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/deepl"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/gemini"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/ollama"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/openai"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/palm"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/proxy"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/replicate"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/tencent"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/vertexai"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/xunfei"
	"github.com/Elysia-SHY/one-api-plus/relay/adaptor/zhipu"
	"github.com/Elysia-SHY/one-api-plus/relay/apitype"
)

func GetAdaptor(apiType int) adaptor.Adaptor {
	switch apiType {
	case apitype.AIProxyLibrary:
		return &aiproxy.Adaptor{}
	case apitype.Ali:
		return &ali.Adaptor{}
	case apitype.Anthropic:
		return &anthropic.Adaptor{}
	case apitype.AwsClaude:
		return &aws.Adaptor{}
	case apitype.Baidu:
		return &baidu.Adaptor{}
	case apitype.Gemini:
		return &gemini.Adaptor{}
	case apitype.OpenAI:
		return &openai.Adaptor{}
	case apitype.PaLM:
		return &palm.Adaptor{}
	case apitype.Tencent:
		return &tencent.Adaptor{}
	case apitype.Xunfei:
		return &xunfei.Adaptor{}
	case apitype.Zhipu:
		return &zhipu.Adaptor{}
	case apitype.Ollama:
		return &ollama.Adaptor{}
	case apitype.Coze:
		return &coze.Adaptor{}
	case apitype.Cohere:
		return &cohere.Adaptor{}
	case apitype.Cloudflare:
		return &cloudflare.Adaptor{}
	case apitype.DeepL:
		return &deepl.Adaptor{}
	case apitype.VertexAI:
		return &vertexai.Adaptor{}
	case apitype.Proxy:
		return &proxy.Adaptor{}
	case apitype.Replicate:
		return &replicate.Adaptor{}
	}
	return nil
}
