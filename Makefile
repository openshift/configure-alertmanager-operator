FIPS_ENABLED=true
include boilerplate/generated-includes.mk

.PHONY: boilerplate-update
boilerplate-update:
	@boilerplate/update

# Include local Makefile libraries
include functions.mk

CATALOG_REGISTRY_ORGANIZATION?=app-sre

.PHONY: skopeo-push
skopeo-push:
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
	$(call create_push_catalog_image,staging,service/saas-configure-alertmanager-operator-bundle,$$APP_SRE_BOT_PUSH_TOKEN,false,service/app-interface,data/services/osd-operators/cicd/saas/saas-$(OPERATOR_NAME).yaml,build/generate-operator-bundle.py,$(CATALOG_REGISTRY_ORGANIZATION))
	$(call create_push_catalog_image,production,service/saas-configure-alertmanager-operator-bundle,$$APP_SRE_BOT_PUSH_TOKEN,true,service/app-interface,data/services/osd-operators/cicd/saas/saas-$(OPERATOR_NAME).yaml,build/generate-operator-bundle.py,$(CATALOG_REGISTRY_ORGANIZATION))
