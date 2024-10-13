PROTO_BASE_DIR := grpc/proto
PROTO_SUBDIRS := $(wildcard $(PROTO_BASE_DIR)/*)
PROTO_FILES := $(foreach dir,$(PROTO_SUBDIRS),$(wildcard $(dir)/*.proto))

all: generate

generate:
	@for dir in $(PROTO_SUBDIRS); do \
		if [ -d $$dir ]; then \
			protoc --proto_path=. --go_out=. --go_opt=paths=source_relative \
			       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
			       $$dir/*.proto; \
			echo "Generated Go code for $$dir"; \
		fi \
	done

clean:
	find $(PROTO_BASE_DIR) -name '*.pb.go' -delete

.PHONY: all generate clean
