package typesafe

import (
	"context"
	"net/http"
)

const modelsURL = "/v1/models"

// ModelCard 描述一个可用模型的元数据。
type ModelCard struct {
	// Name 是可在请求 model 字段中使用的模型名或别名,例如 "jev-latest"。
	Name string `json:"name"`
	// Description 是模型的说明。
	Description string `json:"description"`
	// ReleaseDate 是模型发布时间(RFC 3339 时间戳,如 "2026-09-10T18:38:01.391457+00:00")。
	ReleaseDate string `json:"release_date"`
}

// modelsResponse 是 GET /v1/models 响应的 JSON 形状。
type modelsResponse struct {
	Models []ModelCard `json:"models"`
}

// Models 列出账号当前可用的模型。
//
// 该端点目前只返回别名(如 jev-latest、jev-preview);版本化的模型 ID
// (如 jev-1.13.0)即使未出现在列表中也可直接在 model 字段使用。
func (c *Client) Models(ctx context.Context) ([]ModelCard, error) {
	var resp modelsResponse
	if err := c.roundTrip(ctx, http.MethodGet, modelsURL, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Models, nil
}
