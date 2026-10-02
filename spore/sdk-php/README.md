# Spore

Control plane for the Spore transactional email platform.

For more information, please visit [https://sporee.fr](https://sporee.fr).

## Installation

PHP 8.1+ with the `curl`, `json` and `mbstring` extensions.

```sh
composer require lalternative/spore
```

Until `lalternative/spore` is on Packagist, declare it from the monorepo in
`composer.json` (Composer reads `composer.json` only at a repository root, so a
plain `vcs` repository cannot point at `spore/sdk-php`):

```json
{
  "repositories": [
    {
      "type": "package",
      "package": {
        "name": "lalternative/spore",
        "version": "dev-main",
        "source": {
          "type": "git",
          "url": "https://github.com/lalternativefabrique/packages.git",
          "reference": "main"
        },
        "require": {
          "php": "^8.1",
          "ext-curl": "*",
          "ext-json": "*",
          "ext-mbstring": "*",
          "guzzlehttp/guzzle": "^7.3",
          "guzzlehttp/psr7": "^1.7 || ^2.0"
        },
        "autoload": {
          "psr-4": { "Lalternative\\Spore\\": "spore/sdk-php/lib/" }
        }
      }
    }
  ],
  "require": {
    "lalternative/spore": "dev-main"
  }
}
```

Then run `composer install`.

## Getting Started

```php
<?php
require_once __DIR__ . '/vendor/autoload.php';

use Lalternative\Spore\ApiException;
use Lalternative\Spore\Configuration;
use Lalternative\Spore\Api\AccountApi;
use Lalternative\Spore\Api\MessagingApi;
use Lalternative\Spore\Model\SendEmailSendEmailRequest;

$config = Configuration::getDefaultConfiguration()
    ->setApiKey('Authorization', getenv('SPORE_API_KEY'))
    ->setApiKeyPrefix('Authorization', 'Bearer');

try {
    $me = (new AccountApi(null, $config))->getMe();
    $sent = (new MessagingApi(null, $config))->sendEmail(new SendEmailSendEmailRequest([
        'from' => 'hello@your-domain.com',
        'to' => ['someone@example.com'],
        'subject' => 'Hello',
        'text' => 'Sent with Spore.',
    ]));
    print_r($sent);
} catch (ApiException $e) {
    echo 'Spore API error: ', $e->getMessage(), PHP_EOL;
}
```

## API Endpoints

All URIs are relative to *https://api.sporee.fr*

Class | Method | HTTP request | Description
------------ | ------------- | ------------- | -------------
*AccountApi* | [**deleteAccount**](docs/Api/AccountApi.md#deleteaccount) | **DELETE** /account | Delete your own account
*AccountApi* | [**getMe**](docs/Api/AccountApi.md#getme) | **GET** /me | The account the caller acts on
*ApiKeysApi* | [**createApiKey**](docs/Api/ApiKeysApi.md#createapikey) | **POST** /api-keys | Create an API key
*ApiKeysApi* | [**listApiKeys**](docs/Api/ApiKeysApi.md#listapikeys) | **GET** /api-keys | List API keys
*ApiKeysApi* | [**revokeApiKey**](docs/Api/ApiKeysApi.md#revokeapikey) | **DELETE** /api-keys/{id} | Revoke an API key
*BillingApi* | [**cancelBillingPendingChange**](docs/Api/BillingApi.md#cancelbillingpendingchange) | **DELETE** /billing/pending-change | Withdraw a scheduled plan change
*BillingApi* | [**cancelBillingSubscription**](docs/Api/BillingApi.md#cancelbillingsubscription) | **DELETE** /billing/subscription | Cancel the subscription
*BillingApi* | [**downgradeBillingSubscription**](docs/Api/BillingApi.md#downgradebillingsubscription) | **POST** /billing/downgrade | Schedule a downgrade to a smaller plan
*BillingApi* | [**getBillingCatalogue**](docs/Api/BillingApi.md#getbillingcatalogue) | **GET** /billing/catalogue | Read the public plan catalogue
*BillingApi* | [**getBillingCheckoutSession**](docs/Api/BillingApi.md#getbillingcheckoutsession) | **GET** /billing/checkout/{session_id} | Read how a checkout ended
*BillingApi* | [**getBillingState**](docs/Api/BillingApi.md#getbillingstate) | **GET** /billing/state | Get the calling tenant&#39;s billing state
*BillingApi* | [**listBillingCheckoutMethods**](docs/Api/BillingApi.md#listbillingcheckoutmethods) | **GET** /billing/checkout/methods | List how a plan may be paid for
*BillingApi* | [**lungorWebhook**](docs/Api/BillingApi.md#lungorwebhook) | **POST** /billing/webhook/lungor | Lungor subscription webhook
*BillingApi* | [**quoteBillingUpgrade**](docs/Api/BillingApi.md#quotebillingupgrade) | **POST** /billing/upgrade/quote | Quote what an upgrade would cost
*BillingApi* | [**startBillingCheckout**](docs/Api/BillingApi.md#startbillingcheckout) | **POST** /billing/checkout | Open a checkout for a paid plan
*BillingApi* | [**upgradeBillingSubscription**](docs/Api/BillingApi.md#upgradebillingsubscription) | **POST** /billing/upgrade | Upgrade to a larger plan
*BrandingApi* | [**extractBrand**](docs/Api/BrandingApi.md#extractbrand) | **POST** /identities/{id}/brand/extract | Extract a branding suggestion from a URL or HTML
*BrandingApi* | [**getBrand**](docs/Api/BrandingApi.md#getbrand) | **GET** /identities/{id}/brand | Get the branding for a sending identity
*BrandingApi* | [**setBrand**](docs/Api/BrandingApi.md#setbrand) | **PUT** /identities/{id}/brand | Set the branding for a sending identity
*IdentitiesApi* | [**addIdentityAddress**](docs/Api/IdentitiesApi.md#addidentityaddress) | **POST** /identities/{id}/addresses | Add an allowed From address (local-part) on a verified identity
*IdentitiesApi* | [**createIdentity**](docs/Api/IdentitiesApi.md#createidentity) | **POST** /identities | Register a sending identity
*IdentitiesApi* | [**deleteIdentity**](docs/Api/IdentitiesApi.md#deleteidentity) | **DELETE** /identities/{id} | Delete a sending identity
*IdentitiesApi* | [**disableIdentityAddress**](docs/Api/IdentitiesApi.md#disableidentityaddress) | **POST** /identities/{id}/addresses/{localPart}/disable | Disable an allowed From address
*IdentitiesApi* | [**getIdentity**](docs/Api/IdentitiesApi.md#getidentity) | **GET** /identities/{id} | Get a sending identity
*IdentitiesApi* | [**listIdentities**](docs/Api/IdentitiesApi.md#listidentities) | **GET** /identities | List sending identities
*IdentitiesApi* | [**removeIdentityAddress**](docs/Api/IdentitiesApi.md#removeidentityaddress) | **DELETE** /identities/{id}/addresses/{localPart} | Remove an allowed From address
*IdentitiesApi* | [**verifyIdentity**](docs/Api/IdentitiesApi.md#verifyidentity) | **POST** /identities/{id}/verify | Verify a sending identity&#39;s DNS records
*InboundApi* | [**getInboundMessage**](docs/Api/InboundApi.md#getinboundmessage) | **GET** /inbound/messages/{id} | Get one inbound message
*InboundApi* | [**listInboundMessages**](docs/Api/InboundApi.md#listinboundmessages) | **GET** /inbound/messages | List inbound messages
*InternalApi* | [**ingestBounce**](docs/Api/InternalApi.md#ingestbounce) | **POST** /internal/bounces | Ingest a raw RFC 3464 bounce DSN
*InternalApi* | [**ingestInboundMessage**](docs/Api/InternalApi.md#ingestinboundmessage) | **POST** /internal/inbound | Ingest a raw RFC 5322 inbound message
*InternalApi* | [**setTenantPlan**](docs/Api/InternalApi.md#settenantplan) | **POST** /internal/tenant/plan | Assign a plan to a tenant (service-to-service)
*MessagingApi* | [**getEmail**](docs/Api/MessagingApi.md#getemail) | **GET** /emails/{id} | Get a message
*MessagingApi* | [**listEmails**](docs/Api/MessagingApi.md#listemails) | **GET** /emails | List enqueued/sent messages
*MessagingApi* | [**sendEmail**](docs/Api/MessagingApi.md#sendemail) | **POST** /emails | Enqueue a transactional email
*MessagingApi* | [**sendTestEmail**](docs/Api/MessagingApi.md#sendtestemail) | **POST** /emails/test | Send a diagnostic test email from the UI
*PlansApi* | [**getTenantPlan**](docs/Api/PlansApi.md#gettenantplan) | **GET** /tenant/plan | Get the calling tenant&#39;s plan
*ReputationApi* | [**getReputation**](docs/Api/ReputationApi.md#getreputation) | **GET** /reputation | Get the current sending reputation for the authenticated tenant
*ReputationApi* | [**unfreezeTenantReputation**](docs/Api/ReputationApi.md#unfreezetenantreputation) | **POST** /internal/reputation/{tenantId}/unfreeze | Lift a sending freeze on a tenant
*SuppressionsApi* | [**addSuppression**](docs/Api/SuppressionsApi.md#addsuppression) | **POST** /suppressions | Add an email to the suppression list
*SuppressionsApi* | [**listSuppressions**](docs/Api/SuppressionsApi.md#listsuppressions) | **GET** /suppressions | List suppressed emails
*SuppressionsApi* | [**removeSuppression**](docs/Api/SuppressionsApi.md#removesuppression) | **DELETE** /suppressions/{email} | Remove an email from the suppression list
*TemplatesApi* | [**listTemplates**](docs/Api/TemplatesApi.md#listtemplates) | **GET** /templates | List built-in email templates
*TemplatesApi* | [**previewTemplate**](docs/Api/TemplatesApi.md#previewtemplate) | **POST** /templates/{id}/preview | Render a preview of a template
*UnsubscribeApi* | [**confirmUnsubscribe**](docs/Api/UnsubscribeApi.md#confirmunsubscribe) | **POST** /unsubscribe | One-click unsubscribe (RFC 8058)
*UnsubscribeApi* | [**viewUnsubscribe**](docs/Api/UnsubscribeApi.md#viewunsubscribe) | **GET** /unsubscribe | Render the unsubscribe confirmation page
*UnsubscribesApi* | [**listUnsubscribes**](docs/Api/UnsubscribesApi.md#listunsubscribes) | **GET** /unsubscribes | List unsubscribe events
*UsageApi* | [**getUsage**](docs/Api/UsageApi.md#getusage) | **GET** /usage | Get the calling tenant&#39;s current and historical usage
*WebhooksApi* | [**createWebhookEndpoint**](docs/Api/WebhooksApi.md#createwebhookendpoint) | **POST** /webhooks/endpoints | Create a webhook endpoint
*WebhooksApi* | [**deleteWebhookEndpoint**](docs/Api/WebhooksApi.md#deletewebhookendpoint) | **DELETE** /webhooks/endpoints/{id} | Delete a webhook endpoint
*WebhooksApi* | [**getWebhookEndpoint**](docs/Api/WebhooksApi.md#getwebhookendpoint) | **GET** /webhooks/endpoints/{id} | Get a webhook endpoint
*WebhooksApi* | [**listWebhookEndpoints**](docs/Api/WebhooksApi.md#listwebhookendpoints) | **GET** /webhooks/endpoints | List webhook endpoints
*WebhooksApi* | [**rotateWebhookSecret**](docs/Api/WebhooksApi.md#rotatewebhooksecret) | **POST** /webhooks/endpoints/{id}/rotate-secret | Rotate a webhook endpoint&#39;s signing secret
*WebhooksApi* | [**updateWebhookEndpoint**](docs/Api/WebhooksApi.md#updatewebhookendpoint) | **PATCH** /webhooks/endpoints/{id} | Update a webhook endpoint

## Models

- [AccountMeResponse](docs/Model/AccountMeResponse.md)
- [AddAddressAddAddressRequest](docs/Model/AddAddressAddAddressRequest.md)
- [AddAddressAddAddressResult](docs/Model/AddAddressAddAddressResult.md)
- [AddSuppressionAddSuppressionRequest](docs/Model/AddSuppressionAddSuppressionRequest.md)
- [AddSuppressionResponse](docs/Model/AddSuppressionResponse.md)
- [ApikeysCreateAPIKeyRequest](docs/Model/ApikeysCreateAPIKeyRequest.md)
- [ApikeysCreatedKey](docs/Model/ApikeysCreatedKey.md)
- [ApikeysListAPIKeysResponse](docs/Model/ApikeysListAPIKeysResponse.md)
- [ApikeysView](docs/Model/ApikeysView.md)
- [CancelSubscriptionResult](docs/Model/CancelSubscriptionResult.md)
- [ConfirmUnsubscribeConfirmUnsubscribeRequest](docs/Model/ConfirmUnsubscribeConfirmUnsubscribeRequest.md)
- [ConfirmUnsubscribeResponse](docs/Model/ConfirmUnsubscribeResponse.md)
- [CreateEndpointCreateEndpointRequest](docs/Model/CreateEndpointCreateEndpointRequest.md)
- [CreateEndpointResult](docs/Model/CreateEndpointResult.md)
- [CreateIdentityCreateIdentityRequest](docs/Model/CreateIdentityCreateIdentityRequest.md)
- [CreateIdentityCreateIdentityResult](docs/Model/CreateIdentityCreateIdentityResult.md)
- [CreateIdentityDNSRecord](docs/Model/CreateIdentityDNSRecord.md)
- [DisableAddressDisableAddressRequest](docs/Model/DisableAddressDisableAddressRequest.md)
- [DisableAddressDisableAddressResult](docs/Model/DisableAddressDisableAddressResult.md)
- [DomainPlan](docs/Model/DomainPlan.md)
- [DomainTenantPlan](docs/Model/DomainTenantPlan.md)
- [DomainVariable](docs/Model/DomainVariable.md)
- [DowngradeSubscriptionDowngradeRequest](docs/Model/DowngradeSubscriptionDowngradeRequest.md)
- [DowngradeSubscriptionResult](docs/Model/DowngradeSubscriptionResult.md)
- [EchoHTTPError](docs/Model/EchoHTTPError.md)
- [EventsAttachment](docs/Model/EventsAttachment.md)
- [EventsRecordCheck](docs/Model/EventsRecordCheck.md)
- [ExtractBrandExtractBrandRequest](docs/Model/ExtractBrandExtractBrandRequest.md)
- [ExtractBrandResponse](docs/Model/ExtractBrandResponse.md)
- [GetBillingStateState](docs/Model/GetBillingStateState.md)
- [GetCatalogueCatalogue](docs/Model/GetCatalogueCatalogue.md)
- [GetCataloguePlan](docs/Model/GetCataloguePlan.md)
- [GetCheckoutSessionResult](docs/Model/GetCheckoutSessionResult.md)
- [GetUsageCurrentPeriodView](docs/Model/GetUsageCurrentPeriodView.md)
- [GetUsageHistoryEntry](docs/Model/GetUsageHistoryEntry.md)
- [GetUsageIdentityBreakdown](docs/Model/GetUsageIdentityBreakdown.md)
- [GetUsageUsageResponse](docs/Model/GetUsageUsageResponse.md)
- [IngestBounceResponse](docs/Model/IngestBounceResponse.md)
- [IngestInboundMessageResponse](docs/Model/IngestInboundMessageResponse.md)
- [ListCheckoutMethodsMethod](docs/Model/ListCheckoutMethodsMethod.md)
- [ListCheckoutMethodsResult](docs/Model/ListCheckoutMethodsResult.md)
- [ListEndpointsResult](docs/Model/ListEndpointsResult.md)
- [ListIdentitiesResponse](docs/Model/ListIdentitiesResponse.md)
- [ListInboundMessagesResponse](docs/Model/ListInboundMessagesResponse.md)
- [ListMessagesResult](docs/Model/ListMessagesResult.md)
- [ListSuppressionsResponse](docs/Model/ListSuppressionsResponse.md)
- [ListTemplatesResponse](docs/Model/ListTemplatesResponse.md)
- [ListTemplatesView](docs/Model/ListTemplatesView.md)
- [ListUnsubscribesResponse](docs/Model/ListUnsubscribesResponse.md)
- [PreviewTemplatePreviewTemplateRequest](docs/Model/PreviewTemplatePreviewTemplateRequest.md)
- [PreviewTemplateResponse](docs/Model/PreviewTemplateResponse.md)
- [ProvidersSuggestion](docs/Model/ProvidersSuggestion.md)
- [RepositoryAddressView](docs/Model/RepositoryAddressView.md)
- [RepositoryAttemptView](docs/Model/RepositoryAttemptView.md)
- [RepositoryBrandView](docs/Model/RepositoryBrandView.md)
- [RepositoryEndpointView](docs/Model/RepositoryEndpointView.md)
- [RepositoryIdentityView](docs/Model/RepositoryIdentityView.md)
- [RepositoryInboundMessageView](docs/Model/RepositoryInboundMessageView.md)
- [RepositoryMessageView](docs/Model/RepositoryMessageView.md)
- [RepositoryStateView](docs/Model/RepositoryStateView.md)
- [RepositorySuppressionView](docs/Model/RepositorySuppressionView.md)
- [RepositoryUnsubscribeView](docs/Model/RepositoryUnsubscribeView.md)
- [RotateSecretResult](docs/Model/RotateSecretResult.md)
- [SendEmailQuotaErrorBody](docs/Model/SendEmailQuotaErrorBody.md)
- [SendEmailSendEmailRequest](docs/Model/SendEmailSendEmailRequest.md)
- [SendEmailSendEmailResult](docs/Model/SendEmailSendEmailResult.md)
- [SendEmailUnsubscribeOptions](docs/Model/SendEmailUnsubscribeOptions.md)
- [SendTestEmailSendTestEmailRequest](docs/Model/SendTestEmailSendTestEmailRequest.md)
- [SendTestEmailSendTestEmailResult](docs/Model/SendTestEmailSendTestEmailResult.md)
- [SetBrandResponse](docs/Model/SetBrandResponse.md)
- [SetBrandSetBrandRequest](docs/Model/SetBrandSetBrandRequest.md)
- [SetTenantPlanSetTenantPlanRequest](docs/Model/SetTenantPlanSetTenantPlanRequest.md)
- [StartCheckoutResult](docs/Model/StartCheckoutResult.md)
- [StartCheckoutStartCheckoutRequest](docs/Model/StartCheckoutStartCheckoutRequest.md)
- [UnfreezeResponse](docs/Model/UnfreezeResponse.md)
- [UnfreezeUnfreezeRequest](docs/Model/UnfreezeUnfreezeRequest.md)
- [UpdateEndpointUpdateEndpointRequest](docs/Model/UpdateEndpointUpdateEndpointRequest.md)
- [UpgradeSubscriptionQuote](docs/Model/UpgradeSubscriptionQuote.md)
- [UpgradeSubscriptionQuoteRequest](docs/Model/UpgradeSubscriptionQuoteRequest.md)
- [UpgradeSubscriptionResult](docs/Model/UpgradeSubscriptionResult.md)
- [UpgradeSubscriptionUpgradeRequest](docs/Model/UpgradeSubscriptionUpgradeRequest.md)
- [VerifyIdentityChecksResult](docs/Model/VerifyIdentityChecksResult.md)
- [VerifyIdentityVerifyIdentityResult](docs/Model/VerifyIdentityVerifyIdentityResult.md)

## Authorization

Authentication schemes defined for the API:
### BearerAuth

- **Type**: API key
- **API key parameter name**: Authorization
- **Location**: HTTP header


### InternalToken

- **Type**: API key
- **API key parameter name**: X-Internal-Token
- **Location**: HTTP header


## Tests

To run the tests, use:

```bash
composer install
vendor/bin/phpunit
```

## Author

contact@sporee.fr

## About this package

This PHP package is automatically generated by the [OpenAPI Generator](https://openapi-generator.tech) project:

- API version: `0.1.0`
    - Package version: `0.2.0`
    - Generator version: `7.24.0`
- Build package: `org.openapitools.codegen.languages.PhpClientCodegen`
