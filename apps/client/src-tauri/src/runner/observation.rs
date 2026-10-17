use super::*;

// This identifies the direct Windows process, including the WSL relay. It does
// not prove that an anonymous stdio session can be reattached or transferred.
#[cfg(windows)]
pub(super) fn snapshot(pid: u32) -> Result<Value, String> {
    use windows::Win32::{
        Foundation::{CloseHandle, ERROR_INVALID_PARAMETER, FILETIME, STILL_ACTIVE},
        System::Threading::{
            GetExitCodeProcess, GetProcessTimes, OpenProcess, PROCESS_QUERY_LIMITED_INFORMATION,
        },
    };
    unsafe {
        let handle =
            OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, pid).map_err(|error| {
                if error.code() == windows::core::HRESULT::from_win32(ERROR_INVALID_PARAMETER.0) {
                    "process_not_found".to_owned()
                } else {
                    "process_unverifiable".to_owned()
                }
            })?;
        let result = (|| {
            let mut created = FILETIME::default();
            let mut ended = FILETIME::default();
            let mut kernel = FILETIME::default();
            let mut user = FILETIME::default();
            GetProcessTimes(handle, &mut created, &mut ended, &mut kernel, &mut user)
                .map_err(|_| "process_unverifiable")?;
            let mut code = 0;
            GetExitCodeProcess(handle, &mut code).map_err(|_| "process_unverifiable")?;
            let stamp = ((created.dwHighDateTime as u64) << 32) | created.dwLowDateTime as u64;
            Ok(
                json!({"pid":pid,"created":stamp.to_string(),"running":code == STILL_ACTIVE.0 as u32}),
            )
        })();
        let _ = CloseHandle(handle);
        result
    }
}
#[cfg(not(windows))]
pub(super) fn snapshot(_pid: u32) -> Result<Value, String> {
    Err("process_unverifiable".into())
}
pub(super) fn inspect(metadata: Option<&Value>) -> Value {
    let process = metadata.map(|v| &v["process"]);
    let presence = match process.and_then(|p| p["pid"].as_u64()) {
        Some(pid) if pid <= u32::MAX as u64 => match snapshot(pid as u32) {
            Ok(_) if process.unwrap()["created"].as_str().is_none() => "unverifiable",
            Ok(current) if current["created"] != process.unwrap()["created"] => {
                "different_instance"
            }
            Ok(current) if current["running"] == true => "running",
            Ok(_) => "exited",
            Err(e) if e == "process_not_found" => "not_found",
            Err(_) => "unverifiable",
        },
        _ => "unverifiable",
    };
    json!({"direct_process":presence,"continuation_verified":false,"reason":"stdio_session_not_reattached"})
}

#[cfg(all(test, windows))]
mod tests {
    use super::*;
    #[test]
    fn process_identity_does_not_imply_session_continuation() {
        let process = snapshot(std::process::id()).unwrap();
        let metadata = json!({"process":process});
        assert_eq!(inspect(Some(&metadata))["direct_process"], "running");
        assert_eq!(inspect(Some(&metadata))["continuation_verified"], false);
        let mut other = metadata;
        other["process"]["created"] = json!("different-creation");
        assert_eq!(
            inspect(Some(&other))["direct_process"],
            "different_instance"
        );
        assert_eq!(inspect(None)["direct_process"], "unverifiable");
    }
}
