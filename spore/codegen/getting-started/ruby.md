## Getting Started

```ruby
require 'spore-email'

SporeEmail.configure do |config|
  config.api_key['BearerAuth'] = ENV.fetch('SPORE_API_KEY')
  config.api_key_prefix['BearerAuth'] = 'Bearer'
end

begin
  me = SporeEmail::AccountApi.new.get_me
  sent = SporeEmail::MessagingApi.new.send_email(
    SporeEmail::SendEmailSendEmailRequest.new(
      from: 'hello@your-domain.com',
      to: ['someone@example.com'],
      subject: 'Hello',
      text: 'Sent with Spore.'
    )
  )
  p sent
rescue SporeEmail::ApiError => e
  puts "Spore API error: #{e}"
end
```

