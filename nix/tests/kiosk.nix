{ pkgs, ... }:

let
  # mural exits immediately with "no images found" against an empty content
  # directory, so the fixture needs at least one image for the process to
  # stay running long enough to test the guarded relaunch. A realistically
  # sized image, not a 1x1 pixel — Mural's decodeAndFit path scales to the
  # window size and a degenerate 1x1 source is an untested edge case there.
  testImage = pkgs.runCommand "test-image.png" { nativeBuildInputs = [ pkgs.imagemagick ]; } ''
    magick -size 640x480 xc:skyblue $out
  '';
in
pkgs.testers.nixosTest {
  name = "mural-kiosk";

  nodes.machine =
    { ... }:
    {
      imports = [
        ../modules/mural.nix
        ../modules/kiosk-x11.nix
      ];

      services.mural.enable = true;
      services.mural.restartDelay = 5;

      users.users.mural.hashedPassword = null;

      systemd.tmpfiles.rules = [
        "C /var/lib/mural/content/test.png 0644 mural users - ${testImage}"
      ];
    };

  testScript = ''
    machine.wait_for_unit("greetd.service")

    # retry rather than a single snapshot: an early boot cycle can be
    # transient (VM timing jitter), and the guard is exactly the mechanism
    # that's supposed to recover from that — so tolerate a retry here and
    # only capture the PID once a mural process is actually up.
    get_mural_pid = (
        "for i in $(seq 1 60); do "
        "p=$(pgrep -x mural || true); "
        "if [ -n \"$p\" ]; then echo \"$p\"; exit 0; fi; "
        "sleep 1; done; exit 1"
    )
    pid = machine.succeed(get_mural_pid).strip()

    # crash the app and confirm the guarded relaunch brings it back (as a
    # new PID) rather than leaving a permanently black screen
    machine.succeed(f"kill -9 {pid}")
    machine.wait_until_fails(f"kill -0 {pid} 2>/dev/null")
    new_pid = machine.succeed(get_mural_pid).strip()
    assert new_pid != pid, "expected a new mural PID after the guarded relaunch"
  '';
}
