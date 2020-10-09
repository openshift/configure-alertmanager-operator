# Include boilerplate's generated Makefile libraries
include boilerplate/generated-includes.mk

# TODO: operator-sdk generate wasn't working in this repo prior to
# boilerplate integration, so we're disabling it for now with this
# explicit no-op override. It needs to be fixed -- at a minimum when we
# upgrade to 1.0.0.
op-generate: ;

.PHONY: boilerplate-update
boilerplate-update:
	@boilerplate/update

# Include local Makefile libraries
include functions.mk

CATALOG_REGISTRY_ORGANIZATION?=app-sre

.PHONY: skopeo-push
skopeo-push: build
	skopeo copy \
		--dest-creds "${QUAY_USER}:${QUAY_TOKEN}" \
		"docker-daemon:${OPERATOR_IMAGE_URI_LATEST}" \
		"docker://${OPERATOR_IMAGE_URI_LATEST}"
	skopeo copy \
		--dest-creds "${QUAY_USER}:${QUAY_TOKEN}" \
		"docker-daemon:${OPERATOR_IMAGE_URI}" \
		"docker://${OPERATOR_IMAGE_URI}"

.PHONY: build-catalog-image
build-catalog-image:
	$(call create_push_catalog_image,staging,service/saas-configure-alertmanager-operator-bundle,$$APP_SRE_BOT_PUSH_TOKEN,false,service/saas-osd-operators,$(OPERATOR_NAME)-services/$(OPERATOR_NAME).yaml,build/generate-operator-bundle.py,$(CATALOG_REGISTRY_ORGANIZATION))
	$(call create_push_catalog_image,production,service/saas-configure-alertmanager-operator-bundle,$$APP_SRE_BOT_PUSH_TOKEN,true,service/saas-osd-operators,$(OPERATOR_NAME)-services/$(OPERATOR_NAME).yaml,build/generate-operator-bundle.py,$(CATALOG_REGISTRY_ORGANIZATION))
