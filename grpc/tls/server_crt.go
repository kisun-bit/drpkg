package tls

const ServerCrt = `Certificate:
    Data:
        Version: 3 (0x2)
        Serial Number: 1 (0x1)
    Signature Algorithm: sha256WithRSAEncryption
        Issuer: C=CN, ST=SiChuan, L=Chengdu, O=aio, OU=aio, CN=aio_server
        Validity
            Not Before: Oct 18 06:04:38 2024 GMT
            Not After : Oct 18 06:04:38 2025 GMT
        Subject: C=CN, ST=SiChuan, O=aio, OU=aio, CN=aio
        Subject Public Key Info:
            Public Key Algorithm: rsaEncryption
                Public-Key: (4096 bit)
                Modulus:
                    00:bd:6a:a3:a0:7f:31:97:2b:7b:91:33:50:99:cc:
                    83:5c:1f:01:e4:8d:5f:e9:5b:b5:49:6e:91:9b:4f:
                    fd:ed:2e:0d:4c:fc:6f:8b:86:42:6c:04:2d:3d:1c:
                    da:a0:9d:c0:35:d8:33:c3:b4:de:df:62:bb:43:34:
                    75:99:7c:4f:2a:62:c8:91:f4:71:28:ad:db:6b:bb:
                    cd:59:49:2e:35:0f:48:ad:65:ff:e2:ee:7a:54:8b:
                    dc:d6:28:af:07:50:ec:70:c1:66:c3:7b:ee:08:5c:
                    06:2a:a4:f7:78:f6:ec:8f:7d:6b:8e:a8:23:81:3b:
                    08:4d:aa:d6:00:c2:0f:9c:8c:6a:7a:39:bb:c8:4d:
                    69:4e:bc:6d:08:07:47:c6:5c:11:b2:06:21:93:19:
                    ff:25:ad:df:ad:23:85:4b:dc:0b:5d:e0:ef:d0:8a:
                    b4:ed:8b:35:b5:93:58:a6:da:95:77:e5:e9:c4:ce:
                    4b:74:dd:5f:a0:d4:78:df:99:c4:9d:2d:9d:b2:3b:
                    77:9f:15:a2:de:12:5b:f7:d0:bd:aa:3c:04:ea:d8:
                    c7:bd:e3:ba:c9:37:5d:a5:3b:4d:9c:39:19:7c:ef:
                    7f:b3:c4:de:18:65:41:9c:11:48:ce:24:1e:c6:d6:
                    58:a7:ee:33:8d:a9:1e:3e:69:ea:df:d6:45:4f:ee:
                    78:4f:29:5f:80:78:7e:9a:b1:55:32:3e:70:4b:81:
                    c5:fa:26:ff:78:d4:e9:c2:07:12:fc:da:1f:c4:13:
                    31:ff:12:e2:40:93:c6:68:dc:82:aa:74:20:7c:eb:
                    0a:f6:22:06:18:e9:c9:54:0e:25:58:cc:63:bb:7f:
                    b8:77:1e:0b:dd:fc:6a:40:b1:de:d7:2b:71:4e:16:
                    9a:12:95:77:d1:60:37:26:d3:33:2e:de:c4:96:de:
                    8d:f7:6b:ca:77:e7:27:5b:1a:c3:d0:37:3d:be:7f:
                    ac:1f:a2:ef:c0:e9:0a:1e:7a:13:6e:df:ad:4a:bc:
                    3a:bf:ba:50:37:c5:a7:e6:c6:4d:83:de:40:21:46:
                    14:bd:3c:50:e4:01:2f:15:16:e3:7d:f9:71:fa:9d:
                    f9:68:ad:cc:47:27:0f:43:69:c5:4f:40:04:78:65:
                    5e:68:72:92:d4:2a:a7:a7:c6:7a:62:21:7d:e3:90:
                    87:5f:4c:57:a4:5d:d4:19:41:11:48:02:98:96:4f:
                    0f:91:80:a4:12:32:a2:81:09:f2:ed:85:39:29:29:
                    8e:ed:05:f6:ae:f4:c9:5c:24:f6:20:7a:16:6c:42:
                    24:9c:dd:89:19:b2:09:7d:2d:4e:fc:aa:97:c3:9b:
                    5d:2f:4e:2c:2b:0c:40:16:f3:7f:84:64:87:ad:c0:
                    23:a7:05
                Exponent: 65537 (0x10001)
        X509v3 extensions:
            X509v3 Basic Constraints:
                CA:FALSE
            X509v3 Key Usage:
                Digital Signature, Non Repudiation, Key Encipherment
            X509v3 Subject Alternative Name:
                DNS:*.grpc.aio.com
    Signature Algorithm: sha256WithRSAEncryption
         8c:b7:a6:b7:d8:73:68:2f:0f:e0:26:b5:be:98:57:32:f3:49:
         7d:e8:61:92:90:18:27:66:26:9d:b4:e9:fc:f0:32:69:eb:01:
         39:14:26:be:46:0e:d8:4c:0b:9b:42:37:ef:c9:dc:a3:14:59:
         1e:d7:32:5d:b8:0c:a9:d6:c7:2c:14:5d:32:2a:d3:27:f8:13:
         0b:62:a4:d1:de:65:fc:f3:dc:45:cf:a2:0e:3d:d0:c1:bd:49:
         e4:07:63:d2:e7:0e:5a:71:c2:50:ac:91:0c:f3:58:a8:51:67:
         81:e7:f8:1a:f2:fb:b7:20:77:a7:ae:61:06:0d:c8:1c:08:d0:
         7d:0d:79:a5:d0:c2:94:06:1d:89:a1:dc:7a:db:af:95:13:1e:
         ea:29:f4:b0:ad:27:e3:92:8f:3c:72:27:27:0d:ea:7f:f4:0f:
         32:6e:bf:53:78:56:fe:cd:f6:ec:24:fd:3c:8e:6b:18:1c:71:
         b5:e5:5e:ac:47:7f:c8:2a:8d:e2:8d:10:ff:93:69:8c:10:20:
         a7:e2:6b:b6:d7:73:18:df:d9:03:a9:60:98:7c:19:8e:69:95:
         61:89:6e:52:fc:af:ca:66:0c:bb:24:92:28:bc:29:3c:8f:78:
         6f:83:96:53:8b:07:ac:85:f0:a5:e1:eb:28:c7:ec:ad:3b:3b:
         c8:e4:52:3f:45:55:d7:07:90:fe:6f:d5:ac:92:3a:c3:f1:6d:
         b2:d9:d2:81:f5:37:58:41:b0:54:a1:58:ab:cc:ef:de:68:46:
         dd:5b:74:94:f1:c5:41:5a:c6:7a:32:cb:ad:5b:0b:f0:8f:45:
         2f:c1:cf:45:6c:7c:e3:61:36:bf:e9:2e:19:df:69:fa:93:4d:
         03:8b:97:ae:52:58:eb:f4:d7:49:17:61:aa:9b:54:40:7d:9b:
         79:74:6d:3f:f9:35:a9:20:54:26:3b:47:ba:63:48:ee:d5:8b:
         53:88:49:15:76:0d:8c:a0:5c:51:75:68:4a:a1:98:33:c6:b9:
         a8:fc:95:6a:c4:ae:d8:89:da:e0:d1:b7:22:30:ca:a6:c4:87:
         64:79:19:fa:ca:65:aa:90:dc:fe:60:42:2c:7f:ca:c8:a5:19:
         99:ba:10:eb:4a:c6:aa:96:29:80:8a:5d:6e:50:d3:09:fd:7e:
         a9:3f:b6:71:7c:61:7b:61:4c:af:f1:50:50:59:3d:31:ab:82:
         04:79:7e:1a:94:d9:c9:1b:f0:7e:7b:11:d2:06:20:e4:f9:fa:
         e3:d2:a2:eb:01:82:4f:ee:83:03:e5:ad:3f:dd:6e:d5:75:6a:
         61:0f:ed:d2:7d:20:bf:ca:2e:8a:e2:f4:09:77:d0:bf:ba:d7:
         a9:16:e1:3b:69:67:ca:f3
-----BEGIN CERTIFICATE-----
MIIFWzCCA0OgAwIBAgIBATANBgkqhkiG9w0BAQsFADBiMQswCQYDVQQGEwJDTjEQ
MA4GA1UECAwHU2lDaHVhbjEQMA4GA1UEBwwHQ2hlbmdkdTEMMAoGA1UECgwDYWlv
MQwwCgYDVQQLDANhaW8xEzARBgNVBAMMCmFpb19zZXJ2ZXIwHhcNMjQxMDE4MDYw
NDM4WhcNMjUxMDE4MDYwNDM4WjBJMQswCQYDVQQGEwJDTjEQMA4GA1UECAwHU2lD
aHVhbjEMMAoGA1UECgwDYWlvMQwwCgYDVQQLDANhaW8xDDAKBgNVBAMMA2FpbzCC
AiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAL1qo6B/MZcre5EzUJnMg1wf
AeSNX+lbtUlukZtP/e0uDUz8b4uGQmwELT0c2qCdwDXYM8O03t9iu0M0dZl8Typi
yJH0cSit22u7zVlJLjUPSK1l/+LuelSL3NYorwdQ7HDBZsN77ghcBiqk93j27I99
a46oI4E7CE2q1gDCD5yMano5u8hNaU68bQgHR8ZcEbIGIZMZ/yWt360jhUvcC13g
79CKtO2LNbWTWKbalXfl6cTOS3TdX6DUeN+ZxJ0tnbI7d58Vot4SW/fQvao8BOrY
x73jusk3XaU7TZw5GXzvf7PE3hhlQZwRSM4kHsbWWKfuM42pHj5p6t/WRU/ueE8p
X4B4fpqxVTI+cEuBxfom/3jU6cIHEvzaH8QTMf8S4kCTxmjcgqp0IHzrCvYiBhjp
yVQOJVjMY7t/uHceC938akCx3tcrcU4WmhKVd9FgNybTMy7exJbejfdrynfnJ1sa
w9A3Pb5/rB+i78DpCh56E27frUq8Or+6UDfFp+bGTYPeQCFGFL08UOQBLxUW4335
cfqd+WitzEcnD0NpxU9ABHhlXmhyktQqp6fGemIhfeOQh19MV6Rd1BlBEUgCmJZP
D5GApBIyooEJ8u2FOSkpju0F9q70yVwk9iB6FmxCJJzdiRmyCX0tTvyql8ObXS9O
LCsMQBbzf4Rkh63AI6cFAgMBAAGjNTAzMAkGA1UdEwQCMAAwCwYDVR0PBAQDAgXg
MBkGA1UdEQQSMBCCDiouZ3JwYy5haW8uY29tMA0GCSqGSIb3DQEBCwUAA4ICAQCM
t6a32HNoLw/gJrW+mFcy80l96GGSkBgnZiadtOn88DJp6wE5FCa+Rg7YTAubQjfv
ydyjFFke1zJduAyp1scsFF0yKtMn+BMLYqTR3mX889xFz6IOPdDBvUnkB2PS5w5a
ccJQrJEM81ioUWeB5/ga8vu3IHenrmEGDcgcCNB9DXml0MKUBh2Jodx626+VEx7q
KfSwrSfjko88cicnDep/9A8ybr9TeFb+zfbsJP08jmsYHHG15V6sR3/IKo3ijRD/
k2mMECCn4mu213MY39kDqWCYfBmOaZVhiW5S/K/KZgy7JJIovCk8j3hvg5ZTiwes
hfCl4esox+ytOzvI5FI/RVXXB5D+b9WskjrD8W2y2dKB9TdYQbBUoVirzO/eaEbd
W3SU8cVBWsZ6MsutWwvwj0Uvwc9FbHzjYTa/6S4Z32n6k00Di5euUljr9NdJF2Gq
m1RAfZt5dG0/+TWpIFQmO0e6Y0ju1YtTiEkVdg2MoFxRdWhKoZgzxrmo/JVqxK7Y
idrg0bciMMqmxIdkeRn6ymWqkNz+YEIsf8rIpRmZuhDrSsaqlimAil1uUNMJ/X6p
P7ZxfGF7YUyv8VBQWT0xq4IEeX4alNnJG/B+exHSBiDk+frj0qLrAYJP7oMD5a0/
3W7VdWphD+3SfSC/yi6K4vQJd9C/utepFuE7aWfK8w==
-----END CERTIFICATE-----`
