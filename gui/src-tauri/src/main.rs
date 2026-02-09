// Prevents additional console window on Windows in release, DO NOT REMOVE!!
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::process::{Command, Child};
#[cfg(windows)]
use std::os::windows::process::CommandExt;
use std::sync::Mutex;
use tauri::{CustomMenuItem, SystemTray, SystemTrayMenu, SystemTrayEvent, Manager};

struct CoreProcess(Mutex<Option<Child>>);

fn main() {
    let quit = CustomMenuItem::new("quit".to_string(), "退出");
    let hide = CustomMenuItem::new("hide".to_string(), "隐藏");
    let tray_menu = SystemTrayMenu::new()
        .add_item(hide)
        .add_native_item(tauri::SystemTrayMenuItem::Separator)
        .add_item(quit);

    let system_tray = SystemTray::new().with_menu(tray_menu);

    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, argv, cwd| {
            println!("{}, {argv:?}, {cwd}", app.package_info().name);

            let window = app.get_window("main").unwrap();
            window.show().unwrap();
            window.unminimize().unwrap();
            window.set_focus().unwrap();
        }))
        .manage(CoreProcess(Mutex::new(None)))
        .setup(|app| {
            // Start embedded aetherd
            let core_path = app.path_resolver()
                .resolve_resource("bin/aetherd")
                .or_else(|| app.path_resolver().resolve_resource("bin/aetherd.exe"))
                .expect("failed to resolve aetherd binary");
            
            let mut cmd = Command::new(core_path);
            cmd.arg("--api").arg("127.0.0.1:9880");

            #[cfg(windows)]
            {
                // CREATE_NO_WINDOW = 0x08000000
                cmd.creation_flags(0x08000000);
            }
            
            let child = cmd.spawn()
                .expect("failed to start aetherd");
            
            *app.state::<CoreProcess>().0.lock().unwrap() = Some(child);
            
            Ok(())
        })
        .system_tray(system_tray)
        .on_system_tray_event(|app, event| match event {
            SystemTrayEvent::LeftClick { .. } => {
                let window = app.get_window("main").unwrap();
                window.show().unwrap();
                window.set_focus().unwrap();
            }
            SystemTrayEvent::MenuItemClick { id, .. } => match id.as_str() {
                "quit" => {
                    // Kill core process
                    if let Some(mut child) = app.state::<CoreProcess>().0.lock().unwrap().take() {
                        let _ = child.kill();
                    }
                    std::process::exit(0);
                }
                "hide" => {
                    let window = app.get_window("main").unwrap();
                    window.hide().unwrap();
                }
                _ => {}
            },
            _ => {}
        })
        .on_window_event(|event| match event.event() {
            tauri::WindowEvent::CloseRequested { api, .. } => {
                // Hide to tray instead of closing
                event.window().hide().unwrap();
                api.prevent_close();
            }
            _ => {}
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
