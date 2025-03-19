package main

import (
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

	srv := grpc.NewServer((grpc.Creds(insecure.NewCredentials())))

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
