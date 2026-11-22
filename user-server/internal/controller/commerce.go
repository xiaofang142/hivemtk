package controller

import (
	"errors"
	"github.com/gin-gonic/gin"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"
)

// CommerceController parses DTOs and delegates all commerce decisions to Service.
type CommerceController struct{ svc service.CommerceService }

func NewCommerceController(svc service.CommerceService) *CommerceController {
	return &CommerceController{svc: svc}
}
func commerceHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
}
func commerceError(c *gin.Context, e error) {
	switch {
	case errors.Is(e, service.ErrCommerceInput):
		response.Error(c, http.StatusBadRequest, response.ErrInvalidParams)
	case errors.Is(e, service.ErrCommerceDenied):
		response.Error(c, http.StatusForbidden, response.ErrPermissionDenied)
	case errors.Is(e, service.ErrCommerceConflict), errors.Is(e, service.ErrCommerceBusy):
		response.Error(c, http.StatusConflict, response.ErrInvalidParams)
	case errors.Is(e, service.ErrCommerceProvider):
		response.Error(c, http.StatusServiceUnavailable, response.ErrBusinessError, gin.H{"reason_code": "pending_live_validation"})
	default:
		response.Error(c, http.StatusServiceUnavailable, response.ErrBusinessError)
	}
}

// Profiles returns 200 declared workflow contracts, 403 without tenant identity.
func (c *CommerceController) Profiles(ctx *gin.Context) {
	commerceHeaders(ctx)
	out, e := c.svc.Profiles(ctx.Request.Context(), currentUID(ctx))
	if e != nil {
		commerceError(ctx, e)
		return
	}
	response.Success(ctx, gin.H{"list": out, "total": len(out)}, "ok")
}

// Orders returns 200 masked order context; 400 invalid reference, 403 inaccessible scope.
func (c *CommerceController) Orders(ctx *gin.Context) {
	commerceHeaders(ctx)
	id, e := strconv.ParseUint(ctx.Query("order_id"), 10, 32)
	if e != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams)
		return
	}
	out, e := c.svc.Read(ctx.Request.Context(), currentUID(ctx), dto.CommerceTarget{Profile: ctx.Query("profile"), ShopID: ctx.Query("shop_id"), OrderID: uint(id)})
	if e != nil {
		commerceError(ctx, e)
		return
	}
	response.Success(ctx, out, "ok")
}

// List returns 200 tenant and shop scoped operation summaries, 403 without read membership.
func (c *CommerceController) List(ctx *gin.Context) {
	commerceHeaders(ctx)
	out, e := c.svc.List(ctx.Request.Context(), currentUID(ctx), ctx.Query("profile"), ctx.Query("shop_id"), ctx.Query("state"))
	if e != nil {
		commerceError(ctx, e)
		return
	}
	response.Success(ctx, out, "ok")
}

// Get returns 200 durable result evidence; inaccessible operations never disclose existence.
func (c *CommerceController) Get(ctx *gin.Context) {
	commerceHeaders(ctx)
	out, e := c.svc.Get(ctx.Request.Context(), currentUID(ctx), ctx.Param("id"))
	if e != nil {
		commerceError(ctx, e)
		return
	}
	response.Success(ctx, out, "ok")
}

// Propose returns 202 for a local intent, never a platform completion acknowledgement.
func (c *CommerceController) Propose(ctx *gin.Context) {
	commerceHeaders(ctx)
	var in dto.CommerceProposeRequest
	if !bindMerchantConnection(ctx, &in) {
		return
	}
	out, e := c.svc.Propose(ctx.Request.Context(), currentUID(ctx), in)
	if e != nil {
		commerceError(ctx, e)
		return
	}
	response.Accepted(ctx, out, "ok")
}

// Transition returns 202 with the actual business state; HTTP acceptance never means money moved.
func (c *CommerceController) Transition(ctx *gin.Context) {
	commerceHeaders(ctx)
	var in dto.CommerceTransitionRequest
	if !bindMerchantConnection(ctx, &in) {
		return
	}
	out, e := c.svc.Transition(ctx.Request.Context(), currentUID(ctx), ctx.Param("id"), ctx.Param("verb"), in)
	if e != nil {
		commerceError(ctx, e)
		return
	}
	response.Accepted(ctx, out, "ok")
}

// Evidence stores human provenance separately; 403 protects review role and actor separation.
func (c *CommerceController) Evidence(ctx *gin.Context) {
	commerceHeaders(ctx)
	var in dto.CommerceEvidenceRequest
	if !bindMerchantConnection(ctx, &in) {
		return
	}
	out, e := c.svc.Evidence(ctx.Request.Context(), currentUID(ctx), ctx.Param("id"), false, in)
	if e != nil {
		commerceError(ctx, e)
		return
	}
	response.Accepted(ctx, out, "ok")
}

// Review returns 202 reviewed human provenance; it never upgrades to platform verification.
func (c *CommerceController) Review(ctx *gin.Context) {
	commerceHeaders(ctx)
	var in dto.CommerceEvidenceRequest
	if !bindMerchantConnection(ctx, &in) {
		return
	}
	out, e := c.svc.Evidence(ctx.Request.Context(), currentUID(ctx), ctx.Param("id"), true, in)
	if e != nil {
		commerceError(ctx, e)
		return
	}
	response.Accepted(ctx, out, "ok")
}

// Policy returns 200 new policy version after current administrator validation; stale CAS 409.
func (c *CommerceController) Policy(ctx *gin.Context) {
	commerceHeaders(ctx)
	var in dto.CommercePolicyRequest
	if !bindMerchantConnection(ctx, &in) {
		return
	}
	out, e := c.svc.ConfigurePolicy(ctx.Request.Context(), currentUID(ctx), in)
	if e != nil {
		commerceError(ctx, e)
		return
	}
	response.Success(ctx, out, "ok")
}

// Member returns 200 new shop membership revision after server-side administrator validation.
func (c *CommerceController) Member(ctx *gin.Context) {
	commerceHeaders(ctx)
	var in dto.CommerceMemberRequest
	if !bindMerchantConnection(ctx, &in) {
		return
	}
	out, e := c.svc.ConfigureMember(ctx.Request.Context(), currentUID(ctx), in)
	if e != nil {
		commerceError(ctx, e)
		return
	}
	response.Success(ctx, out, "ok")
}
