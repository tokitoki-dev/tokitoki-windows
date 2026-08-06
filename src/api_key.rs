//! Direct server-side API-key verification (mirrors Go `internal/apikey`).
//!
//! The key travels only in the `Authorization` header — never in the URL,
//! logs, or error messages.

use std::time::Duration;

use serde::Deserialize;

const VERIFY_TIMEOUT: Duration = Duration::from_secs(15);

/// The verification service could not give a definitive answer (transport
/// error, unexpected status, or undecodable body).
#[derive(Debug, thiserror::Error)]
#[error("key verification service unavailable")]
pub struct Unavailable;

#[derive(Deserialize)]
struct VerifyBody {
    #[serde(default)]
    valid: bool,
}

/// Verifies API keys against `POST {base}/api/auth/api-key/verify`.
pub struct Verifier {
    endpoint: String,
    agent: ureq::Agent,
}

impl Verifier {
    /// Creates a verifier for `base_url` (no trailing slash).
    #[must_use]
    pub fn new(base_url: &str) -> Self {
        Self {
            endpoint: format!("{}/api/auth/api-key/verify", base_url.trim_end_matches('/')),
            agent: ureq::AgentBuilder::new().timeout(VERIFY_TIMEOUT).build(),
        }
    }

    /// Returns `Ok(true)` for a valid key and `Ok(false)` for a definitively
    /// invalid/revoked key (HTTP 401).
    ///
    /// # Errors
    /// Returns [`Unavailable`] when no definitive answer was possible.
    pub fn verify(&self, api_key: &str) -> Result<bool, Unavailable> {
        let response = self
            .agent
            .post(&self.endpoint)
            .set("Authorization", &format!("Bearer {api_key}"))
            .set("Accept", "application/json")
            .send_bytes(&[]);
        match response {
            Ok(resp) => resp
                .into_json::<VerifyBody>()
                .map(|body| body.valid)
                .map_err(|_| Unavailable),
            Err(ureq::Error::Status(401, _)) => Ok(false),
            Err(_) => Err(Unavailable),
        }
    }
}
