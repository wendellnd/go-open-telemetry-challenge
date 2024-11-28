# go-open-telemetry-challenge

### Execução

- Adicione sua `WEATHER_API_KEY` na [linha 44](./compose.yaml#L44) do arquivo `compose.yaml`

- Execute o comando `docker compose up --build` para subir a aplicação localmente

- Quando o container estiver no ar, acesse [./api/zipcode.http](./api/zipcode.http) para executar a chamada http para a aplicação

- Acesse o zipkin no endereço http://localhost:9411/ para visualizar as informações dos spans
