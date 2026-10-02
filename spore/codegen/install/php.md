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

