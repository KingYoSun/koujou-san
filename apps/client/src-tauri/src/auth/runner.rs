use super::*;

#[derive(Clone, Deserialize, Serialize)]
pub(crate) struct RuntimeBinding {
    pub api: String,
    pub workspace: String,
    pub owner: String,
    pub auth_file: PathBuf,
    pub root: PathBuf,
}
pub(crate) struct DeviceClient {
    auth: Auth,
    pub binding: RuntimeBinding,
}
impl DeviceClient {
    pub fn open(binding: RuntimeBinding) -> Result<Self, String> {
        let auth = Auth::new(&binding.api, binding.auth_file.clone())?;
        let expected = auth.runner_root_for(&binding.workspace, &binding.owner)?;
        if expected != binding.root {
            return Err("invalid_runner_binding".into());
        }
        Ok(Self { auth, binding })
    }
    pub fn request(
        &self,
        parts: &[&str],
        method: &str,
        body: Value,
        query: &[(&str, &str)],
    ) -> Result<Value, String> {
        self.auth.device_request_as(
            &self.binding.workspace,
            &self.binding.owner,
            parts,
            method,
            body,
            query,
        )
    }
}
impl Auth {
    fn runner_root_for(&self, workspace: &str, user: &str) -> Result<PathBuf, String> {
        Ok(self
            .file
            .parent()
            .ok_or("credential_directory_failed")?
            .join(format!(
                "runner-{}",
                hex_name(&format!("{}\0{workspace}\0{user}", self.api))
            )))
    }
    pub(crate) fn runner_storage(&self, workspace: &str) -> Result<PathBuf, String> {
        let me = self.request("/auth/me", "GET", Value::Null)?;
        self.runner_root_for(
            workspace,
            me["user"]["id"].as_str().ok_or("authentication_required")?,
        )
    }
    pub(crate) fn runner_binding(&self, workspace: &str) -> Result<RuntimeBinding, String> {
        let me = self.request("/auth/me", "GET", Value::Null)?;
        let owner = me["user"]["id"]
            .as_str()
            .ok_or("authentication_required")?
            .to_owned();
        Ok(RuntimeBinding {
            api: self.api.to_string(),
            workspace: workspace.to_owned(),
            root: self.runner_root_for(workspace, &owner)?,
            owner,
            auth_file: self.file.clone(),
        })
    }
    pub(crate) fn runner_request(
        &self,
        workspace: &str,
        parts: &[&str],
        method: &str,
        body: Value,
        query: &[(&str, &str)],
    ) -> Result<Value, String> {
        let me = self.request("/auth/me", "GET", Value::Null)?;
        let owner = me["user"]["id"].as_str().ok_or("authentication_required")?;
        self.device_request_as(workspace, owner, parts, method, body, query)
    }
    fn device_request_as(
        &self,
        workspace: &str,
        owner: &str,
        parts: &[&str],
        method: &str,
        body: Value,
        query: &[(&str, &str)],
    ) -> Result<Value, String> {
        let saved = self.load_device(&self.device_file(workspace, owner))?;
        if saved.token.is_empty() {
            return Err("device_not_registered".into());
        }
        let mut target = self.api.clone();
        target
            .path_segments_mut()
            .map_err(|_| "invalid_API_path")?
            .clear()
            .extend(parts);
        target.query_pairs_mut().extend_pairs(query.iter().copied());
        let mut req = self
            .client
            .request(
                reqwest::Method::from_bytes(method.as_bytes()).map_err(|_| "invalid_method")?,
                target,
            )
            .bearer_auth(&saved.token);
        if !body.is_null() {
            req = req.json(&body)
        }
        let res = req.send().map_err(|_| "api_unavailable")?;
        let status = res.status();
        let value: Value = res.json().map_err(|_| "invalid_runner_response")?;
        if !status.is_success() || value.get("error").is_some() {
            let fallback = if status.as_u16() == 403 {
                "forbidden"
            } else {
                "runner_request_failed"
            };
            return Err(value["error"]["code"]
                .as_str()
                .unwrap_or(fallback)
                .to_owned());
        }
        Ok(value)
    }
}
