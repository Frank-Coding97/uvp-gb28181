package com.uvp.openapi.model;

/**
 * OpenAPIDevicePlaybackActionResult - the contract defines this body as an empty JSON object.
 *
 * <p>Written explicitly because the generator skips object schemas that declare no properties,
 * even when an operation references them, which would leave the API classes referring to a type
 * that does not exist.</p>
 */
public class OpenAPIDevicePlaybackActionResult {

  @Override
  public boolean equals(Object other) {
    return other != null && getClass() == other.getClass();
  }

  @Override
  public int hashCode() {
    return getClass().getName().hashCode();
  }

  @Override
  public String toString() {
    return "OpenAPIDevicePlaybackActionResult{}";
  }
}
