## Getting Started

```python
import os

import spore_email
from spore_email.exceptions import ApiException

configuration = spore_email.Configuration()
configuration.api_key["BearerAuth"] = os.environ["SPORE_API_KEY"]
configuration.api_key_prefix["BearerAuth"] = "Bearer"

with spore_email.ApiClient(configuration) as api_client:
    try:
        me = spore_email.AccountApi(api_client).get_me()
        sent = spore_email.MessagingApi(api_client).send_email(
            spore_email.SendEmailSendEmailRequest(**{
                "from": "hello@your-domain.com",
                "to": ["someone@example.com"],
                "subject": "Hello",
                "text": "Sent with Spore.",
            })
        )
        print(sent)
    except ApiException as e:
        print("Spore API error: %s\n" % e)
```

