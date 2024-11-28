package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type Data struct {
	TemperatureURL  string
	RequestNameOTEL string
	OTELTracer      trace.Tracer
}

type Webserver struct {
	Data *Data
}

// NewServer creates a new server instance
func NewServer(data *Data) *Webserver {
	return &Webserver{
		Data: data,
	}
}

func (we *Webserver) CreateServer() *chi.Mux {
	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Logger)
	router.Use(middleware.Timeout(60 * time.Second))
	// promhttp
	router.Handle("/metrics", promhttp.Handler())
	router.Post("/", we.HandleRequest)
	return router
}

func isValidZipcode(zipCode string) bool {
	return len(zipCode) == 8
}

func (we *Webserver) HandleRequest(w http.ResponseWriter, r *http.Request) {
	carrier := propagation.HeaderCarrier(r.Header)
	ctx := r.Context()
	ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)

	ctx, span := we.Data.OTELTracer.Start(ctx, we.Data.RequestNameOTEL)
	defer span.End()

	var reqPayload map[string]string
	err := json.NewDecoder(r.Body).Decode(&reqPayload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	zipCode, ok := reqPayload["cep"]
	if !ok {
		http.Error(w, "missing zipcode", http.StatusBadRequest)
		return
	}

	if !isValidZipcode(zipCode) {
		http.Error(w, "invalid zipcode", http.StatusBadRequest)
		return
	}

	payload := map[string]string{
		"cep": zipCode,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	body := bytes.NewBuffer(jsonPayload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, we.Data.TemperatureURL, body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if resp.StatusCode != http.StatusOK {
		http.Error(w, string(bodyBytes), resp.StatusCode)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(bodyBytes)
}
