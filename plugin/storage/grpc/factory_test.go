// Copyright (c) 2019 The Jaeger Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package grpc

import (
	"errors"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/uber/jaeger-lib/metrics"
	"go.uber.org/zap"

	"github.com/jaegertracing/jaeger/pkg/config"
	grpcConfig "github.com/jaegertracing/jaeger/plugin/storage/grpc/config"
	"github.com/jaegertracing/jaeger/plugin/storage/grpc/shared"
	"github.com/jaegertracing/jaeger/plugin/storage/grpc/shared/mocks"
	"github.com/jaegertracing/jaeger/storage"
	"github.com/jaegertracing/jaeger/storage/dependencystore"
	dependencyStoreMocks "github.com/jaegertracing/jaeger/storage/dependencystore/mocks"
	"github.com/jaegertracing/jaeger/storage/spanstore"
	spanStoreMocks "github.com/jaegertracing/jaeger/storage/spanstore/mocks"
)

var _ storage.Factory = new(Factory)

type mockPluginBuilder struct {
	plugin *mockPlugin
	err    error
}

func (b *mockPluginBuilder) Build() (shared.StoragePlugin, error) {
	if b.err != nil {
		return nil, b.err
	}
	return b.plugin, nil
}

type mockPlugin struct {
	spanReader       spanstore.Reader
	spanWriter       spanstore.Writer
	archiveReader    shared.ArchiveReader
	archiveWriter    shared.ArchiveWriter
	dependencyReader dependencystore.Reader
}

func (mp *mockPlugin) ArchiveSpanReader() shared.ArchiveReader {
	return mp.archiveReader
}

func (mp *mockPlugin) ArchiveSpanWriter() shared.ArchiveWriter {
	return mp.archiveWriter
}

func (mp *mockPlugin) SpanReader() spanstore.Reader {
	return mp.spanReader
}

func (mp *mockPlugin) SpanWriter() spanstore.Writer {
	return mp.spanWriter
}

func (mp *mockPlugin) DependencyReader() dependencystore.Reader {
	return mp.dependencyReader
}

func TestGRPCStorageFactory(t *testing.T) {
	f := NewFactory()
	v := viper.New()
	f.InitFromViper(v)

	// after InitFromViper, f.builder points to a real plugin builder that will fail in unit tests,
	// so we override it with a mock.
	f.builder = &mockPluginBuilder{
		err: errors.New("made-up error"),
	}
	err := f.Initialize(metrics.NullFactory, zap.NewNop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "made-up error")

	f.builder = &mockPluginBuilder{
		plugin: &mockPlugin{
			spanWriter:       new(spanStoreMocks.Writer),
			spanReader:       new(spanStoreMocks.Reader),
			archiveReader:    new(mocks.ArchiveReader),
			archiveWriter:    new(mocks.ArchiveWriter),
			dependencyReader: new(dependencyStoreMocks.Reader),
		},
	}
	assert.NoError(t, f.Initialize(metrics.NullFactory, zap.NewNop()))

	assert.NotNil(t, f.store)
	reader, err := f.CreateSpanReader()
	assert.NoError(t, err)
	assert.Equal(t, f.store.SpanReader(), reader)
	writer, err := f.CreateSpanWriter()
	assert.NoError(t, err)
	assert.Equal(t, f.store.SpanWriter(), writer)
	depReader, err := f.CreateDependencyReader()
	assert.NoError(t, err)
	assert.Equal(t, f.store.DependencyReader(), depReader)
}

func TestGRPCArchiveStorageFactory(t *testing.T) {
	f := NewFactory()
	v := viper.New()
	f.InitFromViper(v)

	archiveReader := new(mocks.ArchiveReader)
	archiveReader.On("ArchiveSupported", mock.Anything).
		Return(true, nil)
	f.builder = &mockPluginBuilder{
		plugin: &mockPlugin{
			archiveReader: archiveReader,
		},
	}
	assert.NoError(t, f.Initialize(metrics.NullFactory, zap.NewNop()))

	assert.NotNil(t, f.store)
	reader, err := f.CreateArchiveSpanReader()
	assert.NoError(t, err)
	assert.IsType(t, &ArchiveReader{}, reader)
	writer, err := f.CreateArchiveSpanWriter()
	assert.NoError(t, err)
	assert.IsType(t, &ArchiveWriter{}, writer)
}

func TestGRPCArchiveStorageDisabledFactory(t *testing.T) {
	f := NewFactory()
	v := viper.New()
	f.InitFromViper(v)

	archiveReader := new(mocks.ArchiveReader)
	archiveReader.On("ArchiveSupported", mock.Anything).
		Return(false, nil)
	f.builder = &mockPluginBuilder{
		plugin: &mockPlugin{
			archiveReader: archiveReader,
		},
	}
	assert.NoError(t, f.Initialize(metrics.NullFactory, zap.NewNop()))

	assert.NotNil(t, f.store)
	reader, err := f.CreateArchiveSpanReader()
	assert.EqualError(t, err, storage.ErrArchiveStorageNotSupported.Error())
	assert.Nil(t, reader)
	writer, err := f.CreateArchiveSpanWriter()
	assert.EqualError(t, err, storage.ErrArchiveStorageNotSupported.Error())
	assert.Nil(t, writer)
}

func TestWithConfiguration(t *testing.T) {
	f := NewFactory()
	v, command := config.Viperize(f.AddFlags)
	err := command.ParseFlags([]string{
		"--grpc-storage-plugin.binary=noop-grpc-plugin",
		"--grpc-storage-plugin.configuration-file=config.json",
		"--grpc-storage-plugin.log-level=debug",
	})
	assert.NoError(t, err)
	f.InitFromViper(v)
	assert.Equal(t, f.options.Configuration.PluginBinary, "noop-grpc-plugin")
	assert.Equal(t, f.options.Configuration.PluginConfigurationFile, "config.json")
	assert.Equal(t, f.options.Configuration.PluginLogLevel, "debug")
}

func TestInitFromOptions(t *testing.T) {
	f := Factory{}
	o := Options{
		Configuration: grpcConfig.Configuration{
			PluginLogLevel: "info",
		},
	}
	f.InitFromOptions(o)
	assert.Equal(t, o, f.options)
	assert.Equal(t, &o.Configuration, f.builder)
}
