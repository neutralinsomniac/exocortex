.PHONY: tidy

FLAKE = flake.nix
FAKE_HASH = sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=

tidy:
	go mod tidy
	sed -i 's|vendorHash = "sha256-[^"]*"|vendorHash = "$(FAKE_HASH)"|' $(FLAKE)
	REAL=$$(nix build .#exo 2>&1 | awk '/got:/{print $$NF}') && \
		sed -i "s|$(FAKE_HASH)|$$REAL|" $(FLAKE)
	nix run github:numtide/build-go-cache#get-external-imports -- ./. imported-packages
