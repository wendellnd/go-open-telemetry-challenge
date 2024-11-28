package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type Data struct {
	WeatherApiKey   string
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

func (we *Webserver) getLocationByZipcode(ctx context.Context, zipCode string) (string, error) {
	ctx, span := we.Data.OTELTracer.Start(ctx, "viacep-api")
	defer span.End()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://viacep.com.br/ws/%s/json/", zipCode), nil)
	if err != nil {
		return "", err
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var location map[string]string
	err = json.Unmarshal(bodyBytes, &location)
	if err != nil {
		return "", err
	}

	return location["localidade"], nil
}
func (we *Webserver) getTemperatureByLocation(ctx context.Context, location string) (float64, error) {
	ctx, span := we.Data.OTELTracer.Start(ctx, "weather-api")
	defer span.End()

	uri := "https://api.weatherapi.com/v1/current.json"
	uri += "?q=" + url.QueryEscape(location)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		fmt.Println(err)
		return 0, err
	}

	request.Header.Set("accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("key", we.Data.WeatherApiKey)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		rawResponse, _ := io.ReadAll(response.Body)
		fmt.Println(string(rawResponse))

		return 0, nil
	}

	jsonResponse := make(map[string]interface{})
	err = json.NewDecoder(response.Body).Decode(&jsonResponse)
	if err != nil {
		return 0, err
	}

	current, ok := jsonResponse["current"].(map[string]interface{})
	if !ok {
		return 0, nil
	}

	tempC, ok := current["temp_c"].(float64)
	if !ok {
		return 0, nil
	}

	return tempC, nil
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

	location, err := we.getLocationByZipcode(ctx, zipCode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if location == "" {
		http.Error(w, "cannot find zipcode", http.StatusNotFound)
		return
	}

	temperatureC, err := we.getTemperatureByLocation(ctx, location)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tempK := temperatureC + 273.15
	tempF := (temperatureC * 9 / 5) + 32

	response := map[string]interface{}{
		"temp_C": temperatureC,
		"temp_K": tempK,
		"temp_F": tempF,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
	w.WriteHeader(http.StatusOK)
}
