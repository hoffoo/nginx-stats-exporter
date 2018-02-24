FROM golang:alpine

ADD ./ $GOPATH/src/github.com/hoffoo/nginx-status-exporter
RUN cd $GOPATH/src/github.com/hoffoo/nginx-status-exporter && go build -o /nginx-status-exporter

EXPOSE 8080

ENTRYPOINT /nginx-status-exporter
