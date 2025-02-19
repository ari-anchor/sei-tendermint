package tests

import (
	"context"
	"testing"

	"github.com/fortytw2/leaktest"
	"github.com/stretchr/testify/assert"

	abciclientent "github.com/ari-anchor/sei-tendermint/abci/client"
	"github.com/ari-anchor/sei-tendermint/abci/example/kvstore"
	abciserver "github.com/ari-anchor/sei-tendermint/abci/server"
	"github.com/ari-anchor/sei-tendermint/libs/log"
)

func TestClientServerNoAddrPrefix(t *testing.T) {
	t.Cleanup(leaktest.Check(t))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const (
		addr      = "localhost:26658"
		transport = "socket"
	)
	app := kvstore.NewApplication()
	logger := log.NewTestingLogger(t)

	server, err := abciserver.NewServer(logger, addr, transport, app)
	assert.NoError(t, err, "expected no error on NewServer")
	err = server.Start(ctx)
	assert.NoError(t, err, "expected no error on server.Start")
	t.Cleanup(server.Wait)

	client, err := abciclientent.NewClient(logger, addr, transport, true)
	assert.NoError(t, err, "expected no error on NewClient")
	err = client.Start(ctx)
	assert.NoError(t, err, "expected no error on client.Start")
	t.Cleanup(client.Wait)
}
