package volcengine

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ark reports the usage of a stream only when asked to, so a streamed Claude
// format request keeps stream_options through its conversion to chat.
func TestClaudeFormatStreamAsksArkForUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{true, false} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		info := &relaycommon.RelayInfo{
			OriginModelName: "dola-seed-2-1-turbo-260628", RelayFormat: types.RelayFormatClaude, IsStream: stream,
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelType: constant.ChannelTypeVolcEngine, UpstreamModelName: "dola-seed-2-1-turbo-260628", SupportStreamOptions: true,
			},
		}
		request := &dto.ClaudeRequest{Model: "dola-seed-2-1-turbo-260628", Stream: &stream, Messages: []dto.ClaudeMessage{{Role: "user", Content: "hi"}}}
		converted, err := (&Adaptor{}).ConvertClaudeRequest(c, info, request)
		require.NoError(t, err)
		chat, ok := converted.(*dto.GeneralOpenAIRequest)
		require.True(t, ok)
		if stream {
			assert.Equal(t, &dto.StreamOptions{IncludeUsage: true}, chat.StreamOptions)
		} else {
			assert.Nil(t, chat.StreamOptions)
		}
	}
}
