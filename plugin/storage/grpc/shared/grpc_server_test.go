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

package shared

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/jaegertracing/jaeger/model"
	"github.com/jaegertracing/jaeger/plugin/storage/grpc/shared/extra"
	"github.com/jaegertracing/jaeger/plugin/storage/grpc/shared/mocks"
	"github.com/jaegertracing/jaeger/proto-gen/storage_v1"
	grpcMocks "github.com/jaegertracing/jaeger/proto-gen/storage_v1/mocks"
	"github.com/jaegertracing/jaeger/storage/dependencystore"
	dependencyStoreMocks "github.com/jaegertracing/jaeger/storage/dependencystore/mocks"
	"github.com/jaegertracing/jaeger/storage/spanstore"
	spanStoreMocks "github.com/jaegertracing/jaeger/storage/spanstore/mocks"
)

type mockStoragePlugin struct {
	spanReader    *spanStoreMocks.Reader
	spanWriter    *spanStoreMocks.Writer
	archiveReader *spanStoreMocks.Reader
	archiveWriter *spanStoreMocks.Writer
	capabilities  *mocks.PluginCapabilities
	depsReader    *dependencyStoreMocks.Reader
}

func (plugin *mockStoragePlugin) ArchiveSpanReader() spanstore.Reader {
	return plugin.archiveReader
}

func (plugin *mockStoragePlugin) ArchiveSpanWriter() spanstore.Writer {
	return plugin.archiveWriter
}

func (plugin *mockStoragePlugin) SpanReader() spanstore.Reader {
	return plugin.spanReader
}

func (plugin *mockStoragePlugin) SpanWriter() spanstore.Writer {
	return plugin.spanWriter
}

func (plugin *mockStoragePlugin) DependencyReader() dependencystore.Reader {
	return plugin.depsReader
}

func (plugin *mockStoragePlugin) Capabilities() (*extra.Capabilities, error) {
	return plugin.capabilities.Capabilities()
}

type grpcServerTest struct {
	server *grpcServer
	impl   *mockStoragePlugin
}

func withGRPCServer(fn func(r *grpcServerTest)) {
	spanReader := new(spanStoreMocks.Reader)
	spanWriter := new(spanStoreMocks.Writer)
	archiveReader := new(spanStoreMocks.Reader)
	archiveWriter := new(spanStoreMocks.Writer)
	capabilities := new(mocks.PluginCapabilities)
	depReader := new(dependencyStoreMocks.Reader)

	impl := &mockStoragePlugin{
		spanReader:    spanReader,
		spanWriter:    spanWriter,
		archiveReader: archiveReader,
		archiveWriter: archiveWriter,
		depsReader:    depReader,
		capabilities:  capabilities,
	}

	r := &grpcServerTest{
		server: &grpcServer{
			Impl:             impl,
			ArchiveImpl:      impl,
			CapabilitiesImpl: impl,
		},
		impl: impl,
	}
	fn(r)
}

func TestGRPCServerGetServices(t *testing.T) {
	withGRPCServer(func(r *grpcServerTest) {
		r.impl.spanReader.On("GetServices", mock.Anything).
			Return([]string{"service-a"}, nil)

		s, err := r.server.GetServices(context.Background(), &storage_v1.GetServicesRequest{})
		assert.NoError(t, err)
		assert.Equal(t, &storage_v1.GetServicesResponse{Services: []string{"service-a"}}, s)
	})
}

func TestGRPCServerGetOperations(t *testing.T) {
	withGRPCServer(func(r *grpcServerTest) {
		expOperations := []spanstore.Operation{
			{Name: "operation-a", SpanKind: "client"},
			{Name: "operation-a", SpanKind: "server"},
			{Name: "operation-b", SpanKind: "client"},
		}

		r.impl.spanReader.On("GetOperations",
			mock.Anything,
			spanstore.OperationQueryParameters{ServiceName: "service-a"}).
			Return(expOperations, nil)

		resp, err := r.server.GetOperations(context.Background(), &storage_v1.GetOperationsRequest{
			Service: "service-a",
		})
		assert.NoError(t, err)
		assert.Equal(t, len(expOperations), len(resp.Operations))
		for i, operation := range resp.Operations {
			assert.Equal(t, expOperations[i].Name, operation.Name)
			assert.Equal(t, expOperations[i].SpanKind, operation.SpanKind)
		}
	})
}

func TestGRPCServerGetTrace(t *testing.T) {
	withGRPCServer(func(r *grpcServerTest) {
		traceSteam := new(grpcMocks.SpanReaderPlugin_GetTraceServer)
		traceSteam.On("Context").Return(context.Background())
		traceSteam.On("Send", &storage_v1.SpansResponseChunk{Spans: mockTraceSpans}).
			Return(nil)

		var traceSpans []*model.Span
		for i := range mockTraceSpans {
			traceSpans = append(traceSpans, &mockTraceSpans[i])
		}
		r.impl.spanReader.On("GetTrace", mock.Anything, mockTraceID).
			Return(&model.Trace{Spans: traceSpans}, nil)

		err := r.server.GetTrace(&storage_v1.GetTraceRequest{
			TraceID: mockTraceID,
		}, traceSteam)
		assert.NoError(t, err)
	})
}

func TestGRPCServerFindTraces(t *testing.T) {
	withGRPCServer(func(r *grpcServerTest) {
		traceSteam := new(grpcMocks.SpanReaderPlugin_FindTracesServer)
		traceSteam.On("Context").Return(context.Background())
		traceSteam.On("Send", &storage_v1.SpansResponseChunk{Spans: mockTracesSpans[:2]}).
			Return(nil).Once()
		traceSteam.On("Send", &storage_v1.SpansResponseChunk{Spans: mockTracesSpans[2:]}).
			Return(nil).Once()

		var traces []*model.Trace
		var traceID model.TraceID
		trace := &model.Trace{}
		for i, span := range mockTracesSpans {
			if span.TraceID != traceID {
				trace = &model.Trace{}
				traceID = span.TraceID
				traces = append(traces, trace)
			}
			trace.Spans = append(trace.Spans, &mockTracesSpans[i])
		}

		r.impl.spanReader.On("FindTraces", mock.Anything, &spanstore.TraceQueryParameters{}).
			Return(traces, nil)

		err := r.server.FindTraces(&storage_v1.FindTracesRequest{
			Query: &storage_v1.TraceQueryParameters{},
		}, traceSteam)
		assert.NoError(t, err)
	})
}

func TestGRPCServerFindTraceIDs(t *testing.T) {
	withGRPCServer(func(r *grpcServerTest) {
		r.impl.spanReader.On("FindTraceIDs", mock.Anything, &spanstore.TraceQueryParameters{}).
			Return([]model.TraceID{mockTraceID, mockTraceID2}, nil)

		s, err := r.server.FindTraceIDs(context.Background(), &storage_v1.FindTraceIDsRequest{
			Query: &storage_v1.TraceQueryParameters{},
		})
		assert.NoError(t, err)
		assert.Equal(t, &storage_v1.FindTraceIDsResponse{TraceIDs: []model.TraceID{mockTraceID, mockTraceID2}}, s)
	})
}

func TestGRPCServerWriteSpan(t *testing.T) {
	withGRPCServer(func(r *grpcServerTest) {
		r.impl.spanWriter.On("WriteSpan", context.Background(), &mockTraceSpans[0]).
			Return(nil)

		s, err := r.server.WriteSpan(context.Background(), &storage_v1.WriteSpanRequest{
			Span: &mockTraceSpans[0],
		})
		assert.NoError(t, err)
		assert.Equal(t, &storage_v1.WriteSpanResponse{}, s)
	})
}

func TestGRPCServerGetDependencies(t *testing.T) {
	withGRPCServer(func(r *grpcServerTest) {
		lookback := time.Duration(1 * time.Second)
		end := time.Now()
		deps := []model.DependencyLink{
			{
				Source: "source",
				Child:  "child",
			},
		}
		r.impl.depsReader.On("GetDependencies", end, lookback).
			Return(deps, nil)

		s, err := r.server.GetDependencies(context.Background(), &storage_v1.GetDependenciesRequest{
			StartTime: end.Add(-lookback),
			EndTime:   end,
		})
		assert.NoError(t, err)
		assert.Equal(t, &storage_v1.GetDependenciesResponse{Dependencies: deps}, s)
	})
}

func TestGRPCServerGetArchiveTrace(t *testing.T) {
	withGRPCServer(func(r *grpcServerTest) {
		traceSteam := new(grpcMocks.SpanReaderPlugin_GetTraceServer)
		traceSteam.On("Context").Return(context.Background())
		traceSteam.On("Send", &storage_v1.SpansResponseChunk{Spans: mockTraceSpans}).
			Return(nil)

		var traceSpans []*model.Span
		for i := range mockTraceSpans {
			traceSpans = append(traceSpans, &mockTraceSpans[i])
		}
		r.impl.archiveReader.On("GetTrace", mock.Anything, mockTraceID).
			Return(&model.Trace{Spans: traceSpans}, nil)

		err := r.server.GetArchiveTrace(&storage_v1.GetTraceRequest{
			TraceID: mockTraceID,
		}, traceSteam)
		assert.NoError(t, err)
	})
}

func TestGRPCServerGetArchiveTrace_Error(t *testing.T) {
	withGRPCServer(func(r *grpcServerTest) {
		traceSteam := new(grpcMocks.SpanReaderPlugin_GetTraceServer)
		traceSteam.On("Context").Return(context.Background())

		r.impl.archiveReader.On("GetTrace", mock.Anything, mockTraceID).
			Return(nil, fmt.Errorf("some error"))

		err := r.server.GetArchiveTrace(&storage_v1.GetTraceRequest{
			TraceID: mockTraceID,
		}, traceSteam)
		assert.Error(t, err)
	})
}

func TestGRPCServerGetArchiveTrace_StreamError(t *testing.T) {
	withGRPCServer(func(r *grpcServerTest) {
		traceSteam := new(grpcMocks.SpanReaderPlugin_GetTraceServer)
		traceSteam.On("Context").Return(context.Background())
		traceSteam.On("Send", &storage_v1.SpansResponseChunk{Spans: mockTraceSpans}).
			Return(fmt.Errorf("some error"))

		var traceSpans []*model.Span
		for i := range mockTraceSpans {
			traceSpans = append(traceSpans, &mockTraceSpans[i])
		}
		r.impl.archiveReader.On("GetTrace", mock.Anything, mockTraceID).
			Return(&model.Trace{Spans: traceSpans}, nil)

		err := r.server.GetArchiveTrace(&storage_v1.GetTraceRequest{
			TraceID: mockTraceID,
		}, traceSteam)
		assert.Error(t, err)
	})
}

func TestGRPCServerWriteArchiveSpan(t *testing.T) {
	withGRPCServer(func(r *grpcServerTest) {
		r.impl.archiveWriter.On("WriteSpan", &mockTraceSpans[0]).
			Return(nil)

		s, err := r.server.WriteArchiveSpan(context.Background(), &storage_v1.WriteSpanRequest{
			Span: &mockTraceSpans[0],
		})
		assert.NoError(t, err)
		assert.Equal(t, &storage_v1.WriteSpanResponse{}, s)
	})
}

func TestGRPCServerWriteArchiveSpan_Error(t *testing.T) {
	withGRPCServer(func(r *grpcServerTest) {
		r.impl.archiveWriter.On("WriteSpan", &mockTraceSpans[0]).
			Return(fmt.Errorf("some error"))

		_, err := r.server.WriteArchiveSpan(context.Background(), &storage_v1.WriteSpanRequest{
			Span: &mockTraceSpans[0],
		})
		assert.Error(t, err)
	})
}

func TestGRPCServerCapabilities(t *testing.T) {
	withGRPCServer(func(r *grpcServerTest) {
		r.impl.capabilities.On("Capabilities").
			Return(&extra.Capabilities{
				ArchiveSpanReader: true,
				ArchiveSpanWriter: false,
			}, nil)

		capabilities, err := r.server.Capabilities(context.Background(), &storage_v1.CapabilitiesRequest{})
		assert.NoError(t, err)
		assert.Equal(t, &storage_v1.CapabilitiesResponse{ArchiveSpanReader: true, ArchiveSpanWriter: false}, capabilities)
	})
}

func TestGRPCServerCapabilities_Error(t *testing.T) {
	withGRPCServer(func(r *grpcServerTest) {
		r.impl.capabilities.On("Capabilities").
			Return(nil, fmt.Errorf("some error"))

		_, err := r.server.Capabilities(context.Background(), &storage_v1.CapabilitiesRequest{})
		assert.Error(t, err)
	})
}
