// Copyright 2022 Namespace Labs Inc; All rights reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package cluster

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"

	"golang.org/x/sys/unix"
	"namespacelabs.dev/go-ids"
	"namespacelabs.dev/integrations/network/netcopy"
)

type Proxy struct {
	SocketAddr string
	Cleanup    func()
}

type ProxyOpts struct {
	Debug, Errors  io.Writer
	SocketPath     string
	Blocking       bool
	Connect        func(context.Context) (net.Conn, error)
	AnnounceSocket func(string)
}

func RunProxy(ctx context.Context, opts ProxyOpts) (*Proxy, error) {
	if err := unix.Unlink(opts.SocketPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	var d net.ListenConfig
	listener, err := d.Listen(ctx, "unix", opts.SocketPath)
	if err != nil {
		return nil, err
	}

	if opts.AnnounceSocket != nil {
		opts.AnnounceSocket(opts.SocketPath)
	}

	if opts.Blocking {
		ch := make(chan struct{})
		go func() {
			select {
			case <-ch:
			case <-ctx.Done():
			}
			_ = listener.Close()
		}()

		defer close(ch)
		defer os.Remove(opts.SocketPath)

		if err := serveProxy(ctx, opts.Debug, opts.Errors, listener, func(ctx context.Context) (net.Conn, error) {
			return opts.Connect(ctx)
		}); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}

			return nil, err
		}

		return &Proxy{opts.SocketPath, func() {}}, nil
	} else {
		ctxWithCancel, cancel := context.WithCancel(ctx)
		go func() {
			if err := serveProxy(ctxWithCancel, opts.Debug, opts.Errors, listener, func(ctx context.Context) (net.Conn, error) {
				return opts.Connect(ctx)
			}); err != nil {
				log.Fatal(err)
			}
		}()

		return &Proxy{opts.SocketPath, func() {
			cancel()
			os.Remove(opts.SocketPath)
		}}, nil
	}
}

func serveProxy(ctx context.Context, debugLog, errorLog io.Writer, listener net.Listener, connect func(context.Context) (net.Conn, error)) error {
	for {
		rawConn, err := listener.Accept()
		if err != nil {
			return err
		}

		conn := withAddress{rawConn, "local connection"}

		go func() {
			var d netcopy.DebugLogFunc

			id := ids.NewRandomBase32ID(4)
			fmt.Fprintf(debugLog, "[%s] new connection\n", id)

			d = func(format string, args ...any) {
				fmt.Fprintf(debugLog, "["+id+"]: "+format+"\n", args...)
			}

			defer conn.Close()

			peerConn, err := connect(ctx)
			if err != nil {
				fmt.Fprintf(errorLog, "Failed to connect: %v\n", err)
				return
			}

			defer peerConn.Close()

			_ = netcopy.CopyConns(d, conn, peerConn)
		}()
	}
}

type withAddress struct {
	net.Conn
	addrDesc string
}

func (w withAddress) RemoteAddrDebug() string { return w.addrDesc }
