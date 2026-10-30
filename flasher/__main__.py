import queue
import threading
import time
import tkinter as tk
from tkinter import ttk, messagebox, filedialog
from pathlib import Path
import webbrowser
from .device import Device

root = tk.Tk()
root.title('Paperclip Flasher · Experimental preview')
root.geometry('650x520')
frame = ttk.Frame(root, padding=24);frame.pack(fill='both', expand=True)
ttk.Label(frame, text='Paperclip Flasher', font=('', 22, 'bold')).pack(anchor='w')
ttk.Label(frame, text='KDBox 2.2.1 · Windows and Linux · no firmware upgrade', wraplength=590).pack(anchor='w', pady=10)
values = {}
for key, label, secret in [('host','Miner IP address',False),('ssh','SSH root password',True),('web','Dashboard password',True)]:
    ttk.Label(frame,text=label).pack(anchor='w',pady=(10,2))
    values[key]=tk.StringVar()
    ttk.Entry(frame,textvariable=values[key],show='•' if secret else '').pack(fill='x')
status=tk.StringVar(value='Enter your miner details. Only this device will be changed.')
ttk.Label(frame,textvariable=status,wraplength=590).pack(anchor='w',pady=15)
events=queue.Queue()

def confirm_host(host,fingerprint):
    done=threading.Event();result=[]
    def ask():
        result.append(messagebox.askyesno('Confirm miner identity',f'{host}\nSSH fingerprint:\n{fingerprint}\n\nIs this your miner?'))
        done.set()
    events.put(ask);done.wait();return result[0]

def run():
    data={k:(v.get() if k in ['ssh','web'] else v.get().strip()) for k,v in values.items()}
    if not messagebox.askyesno('Device modification risk','Use at your own risk. Firmware-related changes can brick the miner or damage hardware. Rollback cannot guarantee recovery. Back up and install anyway?'):return
    button.configure(state='disabled');restore_button.configure(state='disabled')
    def work():
        device=None
        try:
            device=Device(data['host'],data['ssh'],confirm_host,web_password=data['web'])
            repo=Path(__file__).resolve().parents[1]
            backup=Path.home()/'PaperclipFlasher'/'backups'/time.strftime('%Y%m%d-%H%M%S')
            device.install(repo,data['web'],backup,lambda text:events.put(lambda:status.set(text)))
            events.put(lambda:webbrowser.open('http://'+data['host']+'/'))
        except Exception as exc:
            msg=str(exc);events.put(lambda:status.set('Stopped: '+msg))
        finally:
            if device:device.close()
            events.put(lambda:(button.configure(state='normal'),restore_button.configure(state='normal')))
    threading.Thread(target=work,daemon=True).start()

button=ttk.Button(frame,text='Back up & install Paperclip',command=run);button.pack(anchor='w')
def restore():
    folder=filedialog.askdirectory(title='Choose this miner’s recovery backup')
    if not folder or not messagebox.askyesno('Restore backup','Restore this miner’s saved configuration and restart it?'):return
    host=values['host'].get().strip();password=values['ssh'].get();web_password=values['web'].get();button.configure(state='disabled');restore_button.configure(state='disabled')
    def work():
        device=None
        try:
            device=Device(host,password,confirm_host,web_password=web_password);device.restore(folder);events.put(lambda:status.set('Backup restored. Miner is restarting.'))
        except Exception as exc:
            msg=str(exc);events.put(lambda:status.set('Restore stopped: '+msg))
        finally:
            if device:device.close()
            events.put(lambda:(button.configure(state='normal'),restore_button.configure(state='normal')))
    threading.Thread(target=work,daemon=True).start()
restore_button=ttk.Button(frame,text='Restore a recovery backup',command=restore);restore_button.pack(anchor='w',pady=10)
def pump():
    while not events.empty():events.get()()
    root.after(100,pump)
pump();root.mainloop()
