// Copyright 2019, OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cloudfoundryreceiver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"code.cloudfoundry.org/go-loggregator"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenterror"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/model/pdata"
	"go.opentelemetry.io/collector/obsreport"
	"go.uber.org/zap"
)

const (
	transport              = "http"
	dataFormat             = "cloudfoundry"
	instrumentationLibName = "otelcol/cloudfoundry"
)

var _ component.MetricsReceiver = (*cloudFoundryReceiver)(nil)

// newCloudFoundryReceiver implements the component.MetricsReceiver for Cloud Foundry protocol.
type cloudFoundryReceiver struct {
	logger            *zap.Logger
	cancel            context.CancelFunc
	config            Config
	nextConsumer      consumer.Metrics
	obsrecv           *obsreport.Receiver
	goroutines        sync.WaitGroup
	receiverStartTime time.Time
}

// newCloudFoundryReceiver creates the Cloud Foundry receiver with the given parameters.
func newCloudFoundryReceiver(
	logger *zap.Logger,
	config Config,
	nextConsumer consumer.Metrics) (component.MetricsReceiver, error) {

	if nextConsumer == nil {
		return nil, componenterror.ErrNilNextConsumer
	}

	return &cloudFoundryReceiver{
		logger:            logger,
		config:            config,
		nextConsumer:      nextConsumer,
		obsrecv:           obsreport.NewReceiver(obsreport.ReceiverSettings{ReceiverID: config.ID(), Transport: transport}),
		receiverStartTime: time.Now(),
	}, nil
}

func (cfr *cloudFoundryReceiver) Start(ctx context.Context, host component.Host) error {
	tokenProvider, tokenErr := newUAATokenProvider(cfr.logger, cfr.config.UAA.HTTPClientSettings, cfr.config.UAA.Username, cfr.config.UAA.Password)
	if tokenErr != nil {
		return fmt.Errorf("create cloud foundry UAA token provider: %v", tokenErr)
	}

	streamFactory, streamErr := newEnvelopeStreamFactory(
		cfr.logger,
		tokenProvider,
		cfr.config.RLPGateway.HTTPClientSettings,
		host,
	)
	if streamErr != nil {
		return fmt.Errorf("failed to create cloud foundry RLP envelope stream factory: %v", streamErr)
	}

	innerCtx, cancel := context.WithCancel(ctx)
	cfr.cancel = cancel

	cfr.goroutines.Add(1)

	go func() {
		defer cfr.goroutines.Done()
		cfr.logger.Debug("cloud foundry receiver starting")

		_, tokenErr = tokenProvider.ProvideToken()
		if tokenErr != nil {
			host.ReportFatalError(fmt.Errorf("cloud foundry receiver failed to fetch initial token from UAA: %v", tokenErr))
			return
		}

		envelopeStream, err := streamFactory.CreateStream(innerCtx, cfr.config.RLPGateway.ShardID)
		if err != nil {
			host.ReportFatalError(fmt.Errorf("failed to create RLP gateway envelope stream: %v", err))
			return
		}

		cfr.streamMetrics(innerCtx, envelopeStream, host)
	}()

	return nil
}

func (cfr *cloudFoundryReceiver) Shutdown(_ context.Context) error {
	cfr.cancel()
	return nil
}

func (cfr *cloudFoundryReceiver) streamMetrics(
	ctx context.Context,
	stream loggregator.EnvelopeStream,
	host component.Host) {

	for {
		contextErr := ctx.Err()

		if contextErr != nil {
			cfr.logger.Debug("cloudfoundry metrics streamer stopped gracefully")
			return
		}

		envelopes := stream()
		if envelopes == nil {
			if ctx.Err() != context.Canceled {
				host.ReportFatalError(fmt.Errorf("RLP gateway streamer shut down"))
			}

			return
		}

		metrics := pdata.NewMetrics()
		libraryMetrics := createLibraryMetricsSlice(metrics)

		for _, envelope := range envelopes {
			if envelope != nil {
				// There is concept of startTime in CF loggregator, and we do not know the uptime of the component from
				// which the metric originates, so just provide receiver start time as metric start time
				convertEnvelopeToMetrics(envelope, libraryMetrics, cfr.receiverStartTime)
			}
		}

		if libraryMetrics.Len() > 0 {
			obsCtx := cfr.obsrecv.StartMetricsOp(ctx)
			err := cfr.nextConsumer.ConsumeMetrics(ctx, metrics)
			cfr.obsrecv.EndMetricsOp(obsCtx, dataFormat, metrics.DataPointCount(), err)
		}
	}
}

func createLibraryMetricsSlice(metrics pdata.Metrics) pdata.MetricSlice {
	resourceMetrics := metrics.ResourceMetrics()
	resourceMetric := resourceMetrics.AppendEmpty()
	resourceMetric.Resource().Attributes()
	libraryMetricsSlice := resourceMetric.InstrumentationLibraryMetrics()
	libraryMetrics := libraryMetricsSlice.AppendEmpty()
	libraryMetrics.InstrumentationLibrary().SetName(instrumentationLibName)
	return libraryMetrics.Metrics()
}
