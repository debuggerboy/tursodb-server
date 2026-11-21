POST http://127.0.0.1:8080/v2/pipeline
Content-Type: application/json

{ "baton": null, "requests": [ { "type": "execute", "stmt": { "sql": "SELECT * FROM server_info", "want_rows": true } } ] }
