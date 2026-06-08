// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package redfisheventreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfisheventreceiver"

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/julienschmidt/httprouter"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componentstatus"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/receiverhelper"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfisheventreceiver/internal/metadata"
)

var (
	errNilLogsConsumer      = errors.New("missing a logs consumer")
	errInvalidRequestMethod = errors.New("invalid method; valid method is POST")
	errEmptyResponseBody    = errors.New("request body content length is zero")
)

const healthyResponse = `{"status":"redfishevent receiver is healthy"}`

type eventReceiver struct {
	settings    receiver.Settings
	cfg         *Config
	logConsumer consumer.Logs
	server      *http.Server
	shutdownWG  sync.WaitGroup
	obsrecv     *receiverhelper.ObsReport
}

func newLogsReceiver(params receiver.Settings, cfg *Config, next consumer.Logs) (receiver.Logs, error) {
	if next == nil {
		return nil, errNilLogsConsumer
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	transport := "http"
	if cfg.TLS.HasValue() {
		transport = "https"
	}

	obsrecv, err := receiverhelper.NewObsReport(receiverhelper.ObsReportSettings{
		ReceiverID:             params.ID,
		Transport:              transport,
		ReceiverCreateSettings: params,
	})
	if err != nil {
		return nil, err
	}

	return &eventReceiver{
		settings:    params,
		cfg:         cfg,
		logConsumer: next,
		obsrecv:     obsrecv,
	}, nil
}

func (er *eventReceiver) Start(ctx context.Context, host component.Host) error {
	if er.server != nil && er.server.Handler != nil {
		return nil
	}

	ln, err := er.cfg.ToListener(ctx)
	if err != nil {
		return err
	}

	router := httprouter.New()
	router.POST(er.cfg.Path, er.handleReq)
	router.GET(er.cfg.HealthPath, er.handleHealthCheck)

	er.server, err = er.cfg.ToServer(ctx, host.GetExtensions(), er.settings.TelemetrySettings, router)
	if err != nil {
		return err
	}

	readTimeout, err := time.ParseDuration(er.cfg.ReadTimeout)
	if err != nil {
		return err
	}
	writeTimeout, err := time.ParseDuration(er.cfg.WriteTimeout)
	if err != nil {
		return err
	}
	er.server.ReadHeaderTimeout = readTimeout
	er.server.WriteTimeout = writeTimeout

	er.shutdownWG.Add(1)
	go func() {
		defer er.shutdownWG.Done()
		if errHTTP := er.server.Serve(ln); !errors.Is(errHTTP, http.ErrServerClosed) && errHTTP != nil {
			componentstatus.ReportStatus(host, componentstatus.NewFatalErrorEvent(errHTTP))
		}
	}()

	return nil
}

func (er *eventReceiver) Shutdown(context.Context) error {
	if er.server == nil {
		return nil
	}
	err := er.server.Close()
	er.shutdownWG.Wait()
	return err
}

func (er *eventReceiver) handleReq(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	ctx := er.obsrecv.StartLogsOp(r.Context())

	if r.Method != http.MethodPost {
		er.failBadReq(ctx, w, http.StatusBadRequest, errInvalidRequestMethod)
		er.obsrecv.EndLogsOp(ctx, metadata.Type.String(), 0, errInvalidRequestMethod)
		return
	}

	if r.ContentLength == 0 {
		er.failBadReq(ctx, w, http.StatusBadRequest, errEmptyResponseBody)
		er.obsrecv.EndLogsOp(ctx, metadata.Type.String(), 0, errEmptyResponseBody)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, er.cfg.MaxRequestBodySize))
	_ = r.Body.Close()
	if err != nil {
		er.failBadReq(ctx, w, http.StatusBadRequest, err)
		er.obsrecv.EndLogsOp(ctx, metadata.Type.String(), 0, err)
		return
	}

	bmcIP := resolveBMCSourceIP(
		r.Header.Get(er.cfg.SourceHeaders.IP),
		r.Header.Get(er.cfg.SourceHeaders.SenderAddress),
		r.RemoteAddr,
		body,
	)

	src := sourceContext{
		Vendor:   r.Header.Get(er.cfg.SourceHeaders.Vendor),
		IP:       bmcIP,
		Model:    r.Header.Get(er.cfg.SourceHeaders.Model),
		Firmware: r.Header.Get(er.cfg.SourceHeaders.Firmware),
		Hostname: r.Header.Get(er.cfg.SourceHeaders.Hostname),
		Tenant:   r.Header.Get(er.cfg.SourceHeaders.Tenant),
	}

	ld, numLogs, err := payloadToLogs(body, src)
	if err != nil {
		er.failBadReq(ctx, w, http.StatusBadRequest, err)
		er.obsrecv.EndLogsOp(ctx, metadata.Type.String(), 0, err)
		return
	}

	consumerErr := er.logConsumer.ConsumeLogs(ctx, ld)
	if consumerErr != nil {
		er.failBadReq(ctx, w, http.StatusInternalServerError, consumerErr)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	er.obsrecv.EndLogsOp(ctx, metadata.Type.String(), numLogs, consumerErr)
}

func (*eventReceiver) handleHealthCheck(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(healthyResponse))
}

func (er *eventReceiver) failBadReq(_ context.Context, w http.ResponseWriter, status int, err error) {
	w.WriteHeader(status)
	w.Header().Set("Content-Type", "text/plain")
	_, writeErr := w.Write([]byte(err.Error()))
	if writeErr != nil && er.settings.Logger.Core().Enabled(zap.DebugLevel) {
		er.settings.Logger.Debug("failed to write error response", zap.Error(writeErr))
	}
}
