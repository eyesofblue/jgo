package service

import (
	"context"

	demoprotov1 "example.com/demo-proto/gen/pb/demo_proto/v1"
	"example.com/demo-web/api/http/model"
)

type GetUserRequest struct {
	Uid int64 `json:"uid"`
}

func (service *Service) GetUser(ctx context.Context, request GetUserRequest) (model.UserInfo, error) {
	response, err := service.RPC.DemoProto.GetUser(ctx, &demoprotov1.GetUserRequest{Uid: request.Uid})
	if err != nil {
		return model.UserInfo{}, err
	}
	return model.UserInfo{UID: request.Uid, Name: response.GetTraceId()}, nil
}
