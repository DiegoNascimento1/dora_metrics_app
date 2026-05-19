.PHONY: install run build test lint gen-types clean

install:
	npm ci

run:
	npm start

build:
	npm run build

test:
	npm test -- --watch=false --browsers=ChromeHeadless

lint:
	npm run lint

gen-types:
	npm run gen-types

clean:
	rm -rf node_modules dist .angular
