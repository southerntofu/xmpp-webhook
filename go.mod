module github.com/tmsmr/xmpp-webhook

require (
	github.com/pelletier/go-toml/v2 v2.0.0-beta.3 // indirect
	github.com/savsgio/go-logger v1.0.0
	github.com/tmsmr/xmpp-webhook/config v0.0.0-00010101000000-000000000000
	github.com/tmsmr/xmpp-webhook/parser v0.0.0-00010101000000-000000000000
	golang.org/x/crypto v0.0.0-20210220033148-5ea612d1eb83 // indirect
	golang.org/x/net v0.0.0-20210226172049-e18ecbb05110 // indirect
	golang.org/x/text v0.3.5 // indirect
	mellium.im/sasl v0.2.2-0.20190711145101-7aedd692081c
	mellium.im/xmlstream v0.15.3-0.20210221202126-7cc1407dad4c
	mellium.im/xmpp v0.19.1-0.20210901124536-6846e6241769
)

go 1.15

replace github.com/tmsmr/xmpp-webhook/parser => ./parser

replace github.com/tmsmr/xmpp-webhook/config => ./config
