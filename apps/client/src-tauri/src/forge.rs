use reqwest::Url;

fn source_url(value: &str, instance: &str) -> Result<Url, String> {
    let source = Url::parse(value).map_err(|_| "invalid_source_url")?;
    let base = Url::parse(instance).map_err(|_| "invalid_source_instance")?;
    if !matches!(base.scheme(), "http" | "https")
        || !base.username().is_empty()
        || base.password().is_some()
        || base.query().is_some()
        || base.fragment().is_some()
        || source.origin() != base.origin()
        || !source.username().is_empty()
        || source.password().is_some()
        || !source
            .path()
            .starts_with(&format!("{}/", base.path().trim_end_matches('/')))
    {
        return Err("foreign_source_instance".into());
    }
    Ok(source)
}

#[tauri::command]
pub fn open_forge(url: String, instance: String) -> Result<(), String> {
    crate::auth::open_browser(source_url(&url, &instance)?.as_str())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn navigation_stays_in_the_explicit_instance() {
        assert!(
            source_url(
                "https://localhost:58109/proof/private/issues/1",
                "https://localhost:58109"
            )
            .is_ok()
        );
        assert!(
            source_url(
                "http://192.168.1.2:3000/proof/private",
                "http://192.168.1.2:3000"
            )
            .is_ok()
        );
        for source in [
            "file:///C:/Windows",
            "https://other.test/proof",
            "https://localhost:58110/proof",
            "https://key@localhost:58109/proof",
        ] {
            assert!(source_url(source, "https://localhost:58109").is_err());
        }
        assert!(
            source_url(
                "https://localhost/gitea-other/proof",
                "https://localhost/gitea"
            )
            .is_err()
        );
    }
}
