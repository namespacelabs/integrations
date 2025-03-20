package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"namespacelabs.dev/integrations/examples/buildandrun/testserver/proto"
)

var port = flag.Int("port", 15000, "The port to listen on.")

func main() {
	flag.Parse()

	srv := grpc.NewServer(grpc.Creds(insecure.NewCredentials()),
		grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			response, err := handler(ctx, req)
			log.Printf("%s -> %v", info.FullMethod, err)
			return response, err
		}))

	proto.RegisterTestServiceServer(srv, impl{})

	lst, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("listening on %s", lst.Addr())

	if err := srv.Serve(lst); err != nil {
		log.Fatal(err)
	}
}

type impl struct {
	proto.UnimplementedTestServiceServer
}

func (impl impl) Echo(_ context.Context, req *proto.EchoRequest) (*proto.EchoResponse, error) {
	return &proto.EchoResponse{
		Reply: "hello from within: " + req.Request,
	}, nil
}
