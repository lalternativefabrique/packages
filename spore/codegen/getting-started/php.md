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

