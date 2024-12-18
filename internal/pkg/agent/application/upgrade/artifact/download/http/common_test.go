// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package http

import (
	"bytes"
	"context"
	"crypto/sha512"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade/artifact"
	agtversion "github.com/elastic/elastic-agent/pkg/version"
	"github.com/elastic/elastic-agent/testing/pgptest"
)

const (
	sourcePattern = "/downloads/beats/agentbeat/"
	source        = "http://artifacts.elastic.co/downloads/"
)

var (
	version  = agtversion.NewParsedSemVer(7, 5, 1, "", "")
	beatSpec = artifact.Artifact{
		Name:     "agentbeat",
		Cmd:      "agentbeat",
		Artifact: "beats/agentbeat",
	}
)

type testCase struct {
	system string
	arch   string
}

func getTestCases() []testCase {
	// always test random package to save time
	return []testCase{
		{"linux", "32"},
		{"linux", "64"},
		{"linux", "arm64"},
		{"darwin", "32"},
		{"darwin", "64"},
		{"windows", "32"},
		{"windows", "64"},
	}
}

type extResCode map[string]struct {
	resCode int
	count   int
}

type testDials struct {
	extResCode
}

func (td *testDials) withExtResCode(k string, statusCode int, count int) {
	td.extResCode[k] = struct {
		resCode int
		count   int
	}{statusCode, count}
}

func (td *testDials) reset() {
	*td = testDials{extResCode: make(extResCode)}
}

func getElasticCoServer(t *testing.T) (*httptest.Server, []byte, *testDials) {
	td := testDials{extResCode: make(extResCode)}
	correctValues := map[string]struct{}{
		fmt.Sprintf("%s-%s-%s", beatSpec.Cmd, version, "i386.deb"):             {},
		fmt.Sprintf("%s-%s-%s", beatSpec.Cmd, version, "amd64.deb"):            {},
		fmt.Sprintf("%s-%s-%s", beatSpec.Cmd, version, "i686.rpm"):             {},
		fmt.Sprintf("%s-%s-%s", beatSpec.Cmd, version, "x86_64.rpm"):           {},
		fmt.Sprintf("%s-%s-%s", beatSpec.Cmd, version, "linux-x86.tar.gz"):     {},
		fmt.Sprintf("%s-%s-%s", beatSpec.Cmd, version, "linux-arm64.tar.gz"):   {},
		fmt.Sprintf("%s-%s-%s", beatSpec.Cmd, version, "linux-x86_64.tar.gz"):  {},
		fmt.Sprintf("%s-%s-%s", beatSpec.Cmd, version, "windows-x86.zip"):      {},
		fmt.Sprintf("%s-%s-%s", beatSpec.Cmd, version, "windows-x86_64.zip"):   {},
		fmt.Sprintf("%s-%s-%s", beatSpec.Cmd, version, "darwin-x86_64.tar.gz"): {},
	}
	var resp []byte
	content := []byte("anything will do")
	hash := sha512.Sum512(content)
	pub, sig := pgptest.Sign(t, bytes.NewReader(content))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		packageName := r.URL.Path[len(sourcePattern):]

		ext := filepath.Ext(packageName)
		if ext == ".gz" {
			ext = ".tar.gz"
		}
		packageName = strings.TrimSuffix(packageName, ext)
		switch ext {
		case ".sha512":
			resp = []byte(fmt.Sprintf("%x %s", hash, packageName))
		case ".asc":
			resp = sig
		case ".tar.gz", ".zip", ".deb", ".rpm":
			packageName += ext
			resp = content
		default:
			w.WriteHeader(http.StatusNotFound)
			t.Errorf("mock elastic.co server: unknown file extension: %q", ext)
			return
		}

		if _, ok := correctValues[packageName]; !ok {
			t.Errorf("mock elastic.co server: invalid package name: %q", packageName)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte{})
			return
		}

		if v, ok := td.extResCode[ext]; ok && v.count != 0 {
			w.WriteHeader(v.resCode)
			v.count--
			td.extResCode[ext] = v
		}

		_, err := w.Write(resp)
		assert.NoErrorf(t, err, "mock elastic.co server: failes writing response")
	})

	return httptest.NewServer(handler), pub, &td
}

func getElasticCoClient(server *httptest.Server) http.Client {
	return http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, network, s string) (net.Conn, error) {
				_ = s
				return net.Dial(network, server.Listener.Addr().String())
			},
		},
	}
}
