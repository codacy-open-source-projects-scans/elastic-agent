// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package otel

import (
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/receiver"

	// Receivers:
	filelogreceiver "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/filelogreceiver" // for collecting log files
	otlpreceiver "go.opentelemetry.io/collector/receiver/otlpreceiver"

	// Processors:
	attributesprocessor "github.com/open-telemetry/opentelemetry-collector-contrib/processor/attributesprocessor" // for modifying signal attributes
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/filterprocessor"
	k8sattributesprocessor "github.com/open-telemetry/opentelemetry-collector-contrib/processor/k8sattributesprocessor" // for adding k8s metadata
	resourceprocessor "github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourceprocessor"           // for modifying resource attributes
	transformprocessor "github.com/open-telemetry/opentelemetry-collector-contrib/processor/transformprocessor"         // for OTTL processing on logs
	"go.opentelemetry.io/collector/processor/batchprocessor"                                                            // for batching events

	// Exporters:
	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/elasticsearchexporter"
	fileexporter "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/fileexporter" // for e2e tests
	debugexporter "go.opentelemetry.io/collector/exporter/debugexporter"                           // for dev
	"go.opentelemetry.io/collector/exporter/otlpexporter"

	// Extensions
	"go.opentelemetry.io/collector/extension/memorylimiterextension" // for putting backpressure when approach a memory limit
)

func components() (otelcol.Factories, error) {
	var err error
	factories := otelcol.Factories{}

	// Receivers
	factories.Receivers, err = receiver.MakeFactoryMap(
		otlpreceiver.NewFactory(),
		filelogreceiver.NewFactory(),
	)
	if err != nil {
		return otelcol.Factories{}, err
	}

	// Processors
	factories.Processors, err = processor.MakeFactoryMap(
		batchprocessor.NewFactory(),
		resourceprocessor.NewFactory(),
		attributesprocessor.NewFactory(),
		transformprocessor.NewFactory(),
		filterprocessor.NewFactory(),
		k8sattributesprocessor.NewFactory(),
	)
	if err != nil {
		return otelcol.Factories{}, err
	}

	// Exporters
	factories.Exporters, err = exporter.MakeFactoryMap(
		otlpexporter.NewFactory(),
		debugexporter.NewFactory(),
		fileexporter.NewFactory(),
		elasticsearchexporter.NewFactory(),
	)
	if err != nil {
		return otelcol.Factories{}, err
	}

	factories.Extensions, err = extension.MakeFactoryMap(
		memorylimiterextension.NewFactory(),
	)
	if err != nil {
		return otelcol.Factories{}, err
	}

	return factories, err
}
