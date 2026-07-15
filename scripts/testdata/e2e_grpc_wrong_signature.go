package service

import (
	"context"

	pb "example.com/demo-proto/gen/pb/demo_proto/v1"
)

func (h *DemoProtoHandler) GetUser(context.Context, *pb.WrongRequest) (*pb.GetUserResponse, error) {
	return nil, nil
}
