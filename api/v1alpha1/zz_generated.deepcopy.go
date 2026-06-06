package v1alpha1

import runtime "k8s.io/apimachinery/pkg/runtime"

func (in *DispatchFindingSubject) DeepCopyInto(out *DispatchFindingSubject) {
	*out = *in
}

func (in *DispatchFindingSubject) DeepCopy() *DispatchFindingSubject {
	if in == nil {
		return nil
	}
	out := new(DispatchFindingSubject)
	in.DeepCopyInto(out)
	return out
}

func (in *DispatchFindingSpec) DeepCopyInto(out *DispatchFindingSpec) {
	*out = *in
	if in.Related != nil {
		out.Related = make([]DispatchFindingSubject, len(in.Related))
		copy(out.Related, in.Related)
	}
	if in.Recommendations != nil {
		out.Recommendations = make([]string, len(in.Recommendations))
		copy(out.Recommendations, in.Recommendations)
	}
}

func (in *DispatchFindingSpec) DeepCopy() *DispatchFindingSpec {
	if in == nil {
		return nil
	}
	out := new(DispatchFindingSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *DispatchFindingStatus) DeepCopyInto(out *DispatchFindingStatus) {
	*out = *in
	in.FirstSeen.DeepCopyInto(&out.FirstSeen)
	in.LastSeen.DeepCopyInto(&out.LastSeen)
}

func (in *DispatchFindingStatus) DeepCopy() *DispatchFindingStatus {
	if in == nil {
		return nil
	}
	out := new(DispatchFindingStatus)
	in.DeepCopyInto(out)
	return out
}

func (in *DispatchFinding) DeepCopyInto(out *DispatchFinding) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *DispatchFinding) DeepCopy() *DispatchFinding {
	if in == nil {
		return nil
	}
	out := new(DispatchFinding)
	in.DeepCopyInto(out)
	return out
}

func (in *DispatchFinding) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *DispatchFindingList) DeepCopyInto(out *DispatchFindingList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]DispatchFinding, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *DispatchFindingList) DeepCopy() *DispatchFindingList {
	if in == nil {
		return nil
	}
	out := new(DispatchFindingList)
	in.DeepCopyInto(out)
	return out
}

func (in *DispatchFindingList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
