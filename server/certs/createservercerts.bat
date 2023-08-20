openssl req -x509 -newkey rsa:4096 -nodes -days 3650 -keyout ca_key.pem -out ca_cert.pem -subj /C=US/ST=CA/L=SVL/O=gRPC/CN=test-server_ca/ -config .\openssl.cnf -extensions test_ca -sha256
openssl genrsa -out server_key.pem 4096
openssl req -new -key server_key.pem -days 3650 -out server_csr.pem -subj /C=US/ST=CA/L=SVL/O=gRPC/CN=test-server1/ -config .\openssl.cnf -reqexts test_server
openssl x509 -req -in server_csr.pem -CAkey ca_key.pem -CA ca_cert.pem -days 3650 -set_serial 1000 -out server_cert.pem -extfile .\openssl.cnf -extensions test_server -sha256
openssl verify -verbose -CAfile ca_cert.pem  server_cert.pem
del *_csr.pem